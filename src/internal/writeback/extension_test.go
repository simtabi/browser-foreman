package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// fakeBrowser stands in for a browser and its extension: it holds its bookmarks (ext- ids), reads them into
// <browser>-live, and writes a target as a browser would: refusing a stale plan, giving new nodes ids of its own.
type fakeBrowser struct {
	s       store.Store
	tree    store.Tree
	next    int
	writes  int
	changed bool // the browser changes between the plan and the write
}

func (f *fakeBrowser) Run(ctx context.Context, browser string, cmd map[string]any, _ string, _ time.Duration) (json.RawMessage, error) {
	switch cmd["kind"] {
	case "read-bookmarks":
		_, err := Save(ctx, f.s, Slug(browser)+"-live", browser, "import", "read from "+browser+" (extension)", f.tree, time.Now())
		return nil, err
	case "write-bookmarks":
		if f.changed {
			f.tree.Root.Children[1].Children = append(f.tree.Root.Children[1].Children, b("ext-99", "Z", "https://z.example/"))
		}
		if ExtFingerprint(f.tree) != cmd["expect"] {
			return nil, errors.New("this browser's bookmarks changed since the plan was made: nothing was written")
		}
		target, _, err := f.s.State(ctx, cmd["workspace"].(string), 0)
		if err != nil {
			return nil, err
		}
		var conv func(n *store.Node) *store.Node
		conv = func(n *store.Node) *store.Node {
			c := *n
			if strings.HasPrefix(c.ID, "new-") {
				f.next++
				c.ID = fmt.Sprintf("ext-%d", 500+f.next)
			}
			c.Children = nil
			for _, k := range n.Children {
				c.Children = append(c.Children, conv(k))
			}
			return &c
		}
		f.tree = store.Tree{Root: conv(target.Root)}
		f.writes++
		return json.RawMessage(`{"created":1,"moved":2,"steps":3,"backup":"saved"}`), nil
	}
	return nil, fmt.Errorf("unexpected %v", cmd["kind"])
}

func TestWriteThroughTheExtensionVerifiesByPlanningAgain(t *testing.T) {
	s := stored(t)
	ctx := context.Background()
	f := &fakeBrowser{s: s, tree: WithPrefix(theirs(), "ext-")}
	j := Job{Workspace: "main", Base: "original", Mode: Merge}
	th, err := ReadNow(ctx, s, f, "Google Chrome")
	if err != nil {
		t.Fatal(err)
	}
	target, rep, err := PlanTree(ctx, s, j, th)
	if err != nil || rep.Empty() {
		t.Fatalf("plan: %v %+v", err, rep)
	}
	w, err := WriteThrough(ctx, s, f, j, "Google Chrome", th, target, "google-chrome-live", time.Now())
	if err != nil {
		t.Fatalf("%v (drift %+v)", err, w.Drift)
	}
	if f.writes != 1 || w.Created != 1 || w.Steps != 3 || w.Backup != "saved" || w.Target == 0 || !w.Drift.Empty() {
		t.Fatalf("written %+v, writes %d", w, f.writes)
	}
	// Writing the same plan again: the browser already holds it, so planning against it finds nothing.
	th2, _ := ReadNow(ctx, s, f, "Google Chrome")
	if _, again, _ := PlanTree(ctx, s, j, th2); !again.Empty() {
		t.Fatalf("a second plan after the write: %+v", again)
	}
}

func TestWriteThroughRefusesABrowserThatChangedSinceThePlan(t *testing.T) {
	s := stored(t)
	ctx := context.Background()
	f := &fakeBrowser{s: s, tree: WithPrefix(theirs(), "ext-"), changed: true}
	j := Job{Workspace: "main", Base: "original", Mode: Merge}
	th, _ := ReadNow(ctx, s, f, "Google Chrome")
	target, _, _ := PlanTree(ctx, s, j, th)
	if _, err := WriteThrough(ctx, s, f, j, "Google Chrome", th, target, "google-chrome-live", time.Now()); err == nil || !strings.Contains(err.Error(), "changed since") || f.writes != 0 {
		t.Fatalf("a changed browser was written: %v, writes %d", err, f.writes)
	}
}

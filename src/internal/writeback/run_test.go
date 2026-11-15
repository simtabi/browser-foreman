package writeback

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/chromefile"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// profile writes theirs() as a Chrome profile's Bookmarks file in a fresh user data folder and returns its path.
func profile(t *testing.T, sync bool) string {
	t.Helper()
	var conv func(n *store.Node) map[string]any
	conv = func(n *store.Node) map[string]any {
		m := map[string]any{"id": n.ID, "name": n.Title, "guid": "g-" + n.ID, "date_added": "13340000000000000"}
		if n.Kind == store.KindBookmark {
			m["type"], m["url"] = "url", n.URL
			return m
		}
		kids := []any{}
		for _, c := range n.Children {
			kids = append(kids, conv(c))
		}
		m["type"], m["children"] = "folder", kids
		return m
	}
	th := theirs()
	roots := map[string]any{"bookmark_bar": conv(th.Root.Children[0]), "other": conv(th.Root.Children[1]), "synced": conv(th.Root.Children[2])}
	sum, err := chromefile.Checksum(roots)
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{"checksum": sum, "roots": roots, "version": 1}
	if sync {
		doc["sync_metadata"] = "c3luYw=="
	}
	b, _ := json.Marshal(doc)
	dir := filepath.Join(t.TempDir(), "Default")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Bookmarks")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func stored(t *testing.T) store.Store {
	t.Helper()
	s, err := store.Open("json", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, now := context.Background(), time.Now()
	if _, err := Save(ctx, s, "original", "Export", "import", "export", base(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(ctx, s, "main", "Cleanup", "import", "cleanup", ours(), now); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestApplyFileWritesThePlanAndVerifiesIt(t *testing.T) {
	s, path := stored(t), profile(t, false)
	ctx, j := context.Background(), Job{Workspace: "main", Base: "original", Mode: Merge}
	before, _ := os.ReadFile(path)
	p, err := PlanFile(ctx, s, j, path)
	if err != nil || p.SyncOn || p.Running || p.Report.Empty() {
		t.Fatalf("dry run: %v sync %v running %v %+v", err, p.SyncOn, p.Running, p.Report)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("the dry run wrote the file")
	}
	got, err := ApplyFile(ctx, s, j, path, FileOptions{Fingerprint: p.Report.Fingerprint, Backup: "chrome-before-write"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Drift.Empty() {
		t.Fatalf("planning again after the write still changes things: %+v", got.Drift)
	}
	if kept, _ := os.ReadFile(got.Kept); !bytes.Equal(kept, before) {
		t.Fatal("the file as it was is not kept")
	}
	if b, _ := load(ctx, s, "chrome-before-write"); Fingerprint(b) != p.Report.Fingerprint {
		t.Fatal("the backup workspace does not hold the profile as it was")
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), `"Reading"`) || strings.Contains(string(written), "https://c.example/") {
		t.Fatalf("the file does not hold the cleanup: %s", written)
	}
	if !strings.Contains(string(written), `"guid": "g-17"`) {
		t.Fatal("N, added in Chrome after the export, lost its guid")
	}
}

func TestApplyFileRefusesWhatIsNotSafe(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		sync  bool
		setup func(path string)
		j     Job
		o     func(fp string) FileOptions
		want  string
	}{
		{"running", false, func(path string) {
			_ = os.Symlink("host-1", filepath.Join(filepath.Dir(filepath.Dir(path)), "SingletonLock"))
		}, Job{Mode: Merge}, func(fp string) FileOptions { return FileOptions{Fingerprint: fp, Backup: "b"} }, "running"},
		{"syncing", true, nil, Job{Mode: Merge}, func(fp string) FileOptions { return FileOptions{Fingerprint: fp, Backup: "b"} }, "syncs"},
		{"changed since the dry run", false, nil, Job{Mode: Merge}, func(string) FileOptions { return FileOptions{Fingerprint: "old", Backup: "b"} }, "changed"},
		{"replace unconfirmed", false, nil, Job{Mode: Replace}, func(fp string) FileOptions { return FileOptions{Fingerprint: fp, Backup: "b"} }, "confirm that number"},
	}
	for _, c := range cases {
		s, path := stored(t), profile(t, c.sync)
		if c.setup != nil {
			c.setup(path)
		}
		j := c.j
		j.Workspace, j.Base = "main", "original"
		p, err := PlanFile(ctx, s, j, path)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(path)
		_, err = ApplyFile(ctx, s, j, path, c.o(p.Report.Fingerprint), time.Now())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
		if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
			t.Errorf("%s: the file was written", c.name)
		}
		if _, err := s.Workspace(ctx, "b"); err == nil {
			t.Errorf("%s: a backup was made for a write that did not happen", c.name)
		}
	}
	// Confirmed, both go through.
	s, path := stored(t), profile(t, true)
	j := Job{Workspace: "main", Base: "original", Mode: Replace, DropNew: 1}
	p, _ := PlanFile(ctx, s, j, path)
	if _, err := ApplyFile(ctx, s, j, path, FileOptions{Fingerprint: p.Report.Fingerprint, SyncRiskOK: true, Backup: "b"}, time.Now()); err != nil {
		t.Fatalf("a confirmed replace on a syncing profile: %v", err)
	}
}

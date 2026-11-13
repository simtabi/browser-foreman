package edit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func sample() store.Tree {
	return store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "bar", Kind: store.KindFolder, Title: "Bookmarks Bar", Children: []*store.Node{
			{ID: "go", Kind: store.KindBookmark, Title: "Go", URL: "https://go.dev/"},
			{ID: "dev", Kind: store.KindFolder, Title: "Dev", Children: []*store.Node{
				{ID: "gh", Kind: store.KindBookmark, Title: "GitHub", URL: "https://github.com/"},
			}},
			{ID: "mdn", Kind: store.KindBookmark, Title: "MDN", URL: "https://developer.mozilla.org/"},
		}},
		{ID: "other", Kind: store.KindFolder, Title: "Other bookmarks"},
	}}}
}

func ids() func() string { n := 0; return func() string { n++; return fmt.Sprintf("x%d", n) } }

func shape(n *store.Node) string {
	var b strings.Builder
	for i, c := range n.Children {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(c.ID)
		if c.Kind == store.KindFolder && len(c.Children) > 0 {
			b.WriteString("(" + shape(c) + ")")
		}
	}
	return b.String()
}

func s(v string) *string { return &v }
func i(v int) *int       { return &v }

func run(t *testing.T, tr store.Tree, ops ...Op) (store.Tree, []Event) {
	t.Helper()
	out, ev, err := Apply(tr, ops, time.Unix(1_800_000_000, 0), ids())
	if err != nil {
		t.Fatal(err)
	}
	return out, ev
}

func refused(t *testing.T, tr store.Tree, want string, ops ...Op) {
	t.Helper()
	before := shape(tr.Root)
	_, _, err := Apply(tr, ops, time.Now(), ids())
	if err == nil || !IsRefusal(err) || !strings.Contains(err.Error(), want) {
		t.Fatalf("want a refusal containing %q, got %v", want, err)
	}
	if shape(tr.Root) != before {
		t.Fatal("a refused change altered the input tree")
	}
}

func TestCreateUpdate(t *testing.T) {
	tr, ev := run(t, sample(),
		Op{Op: "create", Parent: "dev", Index: i(0), Kind: "bookmark", Title: s("Docs"), URL: s("https://pkg.go.dev/")},
		Op{Op: "create", Parent: "other", Kind: "folder", Title: s("Reading")},
		Op{Op: "update", ID: "go", Title: s("The Go site"), Note: s("start here")})
	if got := shape(tr.Root); got != "bar(go dev(x1 gh) mdn) other(x2)" {
		t.Fatal(got)
	}
	if ev[0].Type != "bookmark.created" || ev[1].Type != "folder.created" || ev[2].Type != "bookmark.updated" {
		t.Fatalf("%+v", ev)
	}
	if Summary(ev) != "Added 2, Edited 1" {
		t.Fatal(Summary(ev))
	}
	if shape(sample().Root) != "bar(go dev(gh) mdn) other" {
		t.Fatal("input mutated")
	}
	refused(t, sample(), "run code", Op{Op: "create", Parent: "bar", Kind: "bookmark", URL: s("javascript:alert(1)")})
	refused(t, sample(), "run code", Op{Op: "update", ID: "go", URL: s("data:text/html,x")})
	refused(t, sample(), "not a folder", Op{Op: "create", Parent: "go", Kind: "folder", Title: s("x")})
	refused(t, sample(), "folder has no URL", Op{Op: "update", ID: "dev", URL: s("https://x/")})
	refused(t, sample(), "unknown change", Op{Op: "explode"})
}

func TestMove(t *testing.T) {
	tr, ev := run(t, sample(), Op{Op: "move", IDs: []string{"mdn", "go"}, Parent: "dev", Index: i(0)})
	if got := shape(tr.Root); got != "bar(dev(mdn go gh)) other" {
		t.Fatal(got)
	}
	if len(ev) != 2 || ev[0].Data["from"] != "bar" || ev[0].Data["to"] != "dev" {
		t.Fatalf("%+v", ev)
	}
	// Within one folder: moving the first item to the end lands it at the end, not one short.
	tr, _ = run(t, sample(), Op{Op: "move", IDs: []string{"go"}, Parent: "bar", Index: i(3)})
	if got := shape(tr.Root); got != "bar(dev(gh) mdn go) other" {
		t.Fatal(got)
	}
	tr, _ = run(t, sample(), Op{Op: "move", IDs: []string{"mdn"}, Parent: "bar", Index: i(0)})
	if got := shape(tr.Root); got != "bar(mdn go dev(gh)) other" {
		t.Fatal(got)
	}
	refused(t, sample(), "into itself", Op{Op: "move", IDs: []string{"bar"}, Parent: "dev"})
	refused(t, sample(), "into itself", Op{Op: "move", IDs: []string{"dev"}, Parent: "dev"})
}

func TestTrashRestorePurge(t *testing.T) {
	tr, _ := run(t, sample(), Op{Op: "trash", IDs: []string{"dev", "go"}, Reason: "duplicate"})
	if got := shape(tr.Root); got != "bar(mdn) other" || len(tr.Trash) != 2 || tr.Trash[0].From != "bar" || tr.Trash[0].Index != 1 {
		t.Fatalf("%s %+v", got, tr.Trash)
	}
	if b, f := tr.Count(); b != 1 || f != 2 {
		t.Fatalf("trash counted as live: %d %d", b, f)
	}
	back, ev := run(t, tr, Op{Op: "restore", IDs: []string{"go", "dev"}})
	if got := shape(back.Root); got != "bar(go dev(gh) mdn) other" || len(back.Trash) != 0 {
		t.Fatalf("restore did not put them back where they were: %s", got)
	}
	if ev[0].Type != "bookmark.restored" {
		t.Fatal(ev)
	}
	// The parent went away: the item lands in the first top folder rather than vanishing.
	tr2, _ := run(t, sample(), Op{Op: "trash", IDs: []string{"gh"}}, Op{Op: "trash", IDs: []string{"dev"}},
		Op{Op: "purge", IDs: []string{"dev"}}, Op{Op: "restore", IDs: []string{"gh"}})
	if got := shape(tr2.Root); got != "bar(go mdn gh) other" {
		t.Fatal(got)
	}
	gone, ev := run(t, tr, Op{Op: "purge"})
	if len(gone.Trash) != 0 || ev[0].Type != "trash.emptied" || ev[0].Data["count"] != 2 {
		t.Fatalf("%+v", ev)
	}
	refused(t, sample(), "top folder", Op{Op: "trash", IDs: []string{"bar"}})
	refused(t, sample(), "not in the Trash", Op{Op: "restore", IDs: []string{"go"}})
	refused(t, sample(), "Trash is empty", Op{Op: "purge"})
	// A refused op later in a batch leaves nothing applied.
	refused(t, sample(), "change 2", Op{Op: "trash", IDs: []string{"go"}}, Op{Op: "move", IDs: []string{"nope"}, Parent: "bar"})
}

func TestTrashKeepsOldHashes(t *testing.T) {
	h1, _, _ := store.Hash(sample())
	tr, _ := run(t, sample(), Op{Op: "trash", IDs: []string{"go"}}, Op{Op: "restore", IDs: []string{"go"}})
	h2, _, _ := store.Hash(tr)
	if h1 != h2 {
		t.Fatal("an empty Trash changed the tree's hash; stored history would no longer dedupe")
	}
}

func TestRatingAndTags(t *testing.T) {
	tr, ev := run(t, sample(), Op{Op: "update", ID: "go", Rating: i(4), Tags: []string{" work ", "Read later", "WORK", "go"}})
	g := tr.Root.Children[0].Children[0]
	if g.Rating != 4 || strings.Join(g.Tags, "|") != "go|Read later|work" {
		t.Fatalf("rating %d tags %q: trimmed, once each, sorted", g.Rating, g.Tags)
	}
	if ev[0].Type != "bookmark.updated" || ev[0].Data["rating"] != 4 {
		t.Fatalf("%+v", ev)
	}
	cleared, _ := run(t, tr, Op{Op: "update", ID: "go", Rating: i(0), Tags: []string{}})
	if g := cleared.Root.Children[0].Children[0]; g.Rating != 0 || g.Tags != nil {
		t.Fatalf("0 and [] clear: %d %q", g.Rating, g.Tags)
	}
	refused(t, sample(), "0 to 5", Op{Op: "update", ID: "go", Rating: i(6)})
	refused(t, sample(), "0 to 5", Op{Op: "update", ID: "go", Rating: i(-1)})
	refused(t, sample(), "comma", Op{Op: "update", ID: "go", Tags: []string{"a,b"}})
	refused(t, sample(), "longer than", Op{Op: "update", ID: "go", Tags: []string{strings.Repeat("x", 41)}})
	many := make([]string, 21)
	for k := range many {
		many[k] = fmt.Sprintf("t%d", k)
	}
	refused(t, sample(), "at most 20", Op{Op: "update", ID: "go", Tags: many})
	refused(t, sample(), "only bookmarks", Op{Op: "update", ID: "dev", Rating: i(3)})
}

func TestDiffChecksRatingAndTags(t *testing.T) {
	next := sample()
	next.Root.Children[0].Children[0].Rating = 5
	next.Root.Children[0].Children[0].Tags = []string{"b", "a", "a"}
	ev, err := Diff(sample(), next)
	if err != nil || len(ev) != 1 || ev[0].Type != "bookmark.updated" || ev[0].Data["rating"] != 5 {
		t.Fatalf("%+v %v", ev, err)
	}
	if got := strings.Join(next.Root.Children[0].Children[0].Tags, "|"); got != "a|b" {
		t.Fatalf("a whole-tree save keeps tags clean: %q", got)
	}
	bad := sample()
	bad.Root.Children[0].Children[0].Rating = 9
	if _, err := Diff(sample(), bad); err == nil || !IsRefusal(err) {
		t.Fatal("a 9-star rating got through a whole-tree save")
	}
}

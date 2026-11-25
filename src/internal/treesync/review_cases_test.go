package treesync

import (
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Cases from the phase 5 review: each produced a tree Commit refuses, or lost bookmarks without a trace.

func fo(id, title string, kids ...*store.Node) *store.Node {
	return &store.Node{ID: id, Kind: store.KindFolder, Title: title, Children: kids}
}
func bk(id, title string) *store.Node {
	return &store.Node{ID: id, Kind: store.KindBookmark, Title: title, URL: "https://" + id + ".example/"}
}

// everywhere counts each id in the tree and the Trash, so a bookmark kept twice or lost shows.
func everywhere(t store.Tree) map[string]int {
	out := map[string]int{}
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		for _, c := range n.Children {
			out[c.ID]++
			walk(c)
		}
	}
	walk(t.Root)
	for _, tr := range t.Trash {
		out[tr.Node.ID]++
		walk(tr.Node)
	}
	return out
}

func valid(t *testing.T, res Result, want ...string) {
	t.Helper()
	if err := res.Tree.Validate(); err != nil {
		t.Fatalf("Commit would refuse it: %v (%s)", err, shape(res.Tree.Root))
	}
	got := everywhere(res.Tree)
	for _, id := range want {
		if got[id] != 1 {
			t.Errorf("%s appears %d times in the tree and Trash (%s)", id, got[id], shape(res.Tree.Root))
		}
	}
}

func TestACrossMoveKeepsBothFolders(t *testing.T) {
	base := store.Tree{Root: fo("root", "", fo("bar", "Bar", fo("A", "A", bk("a1", "a1")), fo("B", "B", bk("b1", "b1"))))}
	ws := store.Tree{Root: fo("root", "", fo("bar", "Bar", fo("B", "B", bk("b1", "b1"), fo("A", "A", bk("a1", "a1")))))}
	br := store.Tree{Root: fo("root", "", fo("bar", "Bar", fo("A", "A", bk("a1", "a1"), fo("B", "B", bk("b1", "b1")))))}
	for _, newer := range []Side{Browser, Workspace} {
		valid(t, Merge3(base, ws, br, newer, 1, "x"), "A", "B", "a1", "b1")
	}
}

func TestMovedOutThenFolderRemoved(t *testing.T) {
	base := store.Tree{Root: fo("root", "", fo("bar", "Bar", fo("F", "F", bk("c", "c"), bk("d", "d"))))}
	br := store.Tree{Root: fo("root", "", fo("bar", "Bar", bk("c", "c")))}
	res := Merge3(base, base, br, Browser, 1, "x")
	valid(t, res, "c", "d", "F")
	if node(res.Tree, "c") == nil {
		t.Fatal("c, moved out before F went, is not in the tree")
	}
}

func TestAddedIntoAFolderTheBrowserRemoved(t *testing.T) {
	base := store.Tree{Root: fo("root", "", fo("bar", "Bar", fo("F", "F", bk("c", "c"))))}
	ws := store.Tree{Root: fo("root", "", fo("bar", "Bar", fo("F", "F", bk("c", "c"), bk("new", "new"))))}
	br := store.Tree{Root: fo("root", "", fo("bar", "Bar"))}
	res := Merge3(base, ws, br, Browser, 1, "x")
	valid(t, res, "new", "F", "c")
	if node(res.Tree, "new") == nil {
		t.Fatal("what the workspace added is not in the tree")
	}
}

func TestTrashedThenEditedInTheBrowserComesBackOnce(t *testing.T) {
	base := store.Tree{Root: fo("root", "", fo("bar", "Bar", bk("x", "x")))}
	ws := store.Tree{Root: fo("root", "", fo("bar", "Bar")), Trash: []*store.Trashed{{Node: bk("x", "x"), From: "bar"}}}
	br := store.Tree{Root: fo("root", "", fo("bar", "Bar", bk("x", "x renamed")))}
	valid(t, Merge3(base, ws, br, Browser, 1, "x"), "x")
}

func TestNothingChangedGivesTheSameTree(t *testing.T) {
	tr := store.Tree{Root: fo("root", "", fo("bar", "Bar", bk("a", "a"), fo("F", "F", bk("b", "b"))))}
	x, _, _ := store.Hash(Merge3(tr, tr, tr, Workspace, 1, "x").Tree)
	y, _, _ := store.Hash(tr)
	if x != y {
		t.Fatal("a merge of three equal trees changed it, so every minute would commit")
	}
}

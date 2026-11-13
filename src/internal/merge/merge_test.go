package merge

import (
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestKeyFoldsWhatOpensTheSamePage(t *testing.T) {
	same := [][2]string{
		{"https://www.Example.com/a/", "https://example.com/a"},
		{"https://example.com/a?utm_source=x&id=3&fbclid=y", "https://example.com/a?id=3"},
		{"https://example.com/a?ref=hn", "https://example.com/a"},
		{"http://example.com/a#:~:text=hello", "http://example.com/a"},
		{"https://example.com", "https://example.com/"},
	}
	for _, p := range same {
		if Key(p[0]) != Key(p[1]) {
			t.Errorf("%s and %s should share a key: %q %q", p[0], p[1], Key(p[0]), Key(p[1]))
		}
	}
	differ := [][2]string{
		{"https://example.com/a#/devices", "https://example.com/a"}, // an app route is another page
		{"https://example.com/a?id=3", "https://example.com/a?id=4"},
		{"https://example.com/A", "https://example.com/a"}, // paths are case-sensitive
		{"https://example.com/a", "http://other.example/a"},
		{"javascript:alert(1)", "javascript:alert(2)"},
	}
	for _, p := range differ {
		if Key(p[0]) == Key(p[1]) {
			t.Errorf("%s and %s must stay apart (key %q)", p[0], p[1], Key(p[0]))
		}
	}
}

func bm(id, title, u string, added int64) *store.Node {
	return &store.Node{ID: id, Kind: store.KindBookmark, Title: title, URL: u, Added: added}
}
func folder(id, title string, kids ...*store.Node) *store.Node {
	return &store.Node{ID: id, Kind: store.KindFolder, Title: title, Children: kids}
}
func tree(kids ...*store.Node) store.Tree { return store.Tree{Root: folder("root", "", kids...)} }

func find(n *store.Node, path ...string) *store.Node {
	for _, p := range path {
		var next *store.Node
		for _, c := range n.Children {
			if c.Title == p {
				next = c
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	return n
}

func TestExistingBookmarksAlwaysWin(t *testing.T) {
	base := tree(folder("bar", "Bookmarks Bar", bm("keep", "Go", "https://go.dev/", 500)))
	src := tree(folder("x", "Bookmarks Bar", folder("y", "Deep", folder("z", "Deeper", bm("n1", "Go docs", "https://www.go.dev", 100)))))
	r, err := Merge(base, []Source{{Name: "Bookmarks.html", Tree: src}}, false, 42)
	if err != nil {
		t.Fatal(err)
	}
	if r.Added != 0 || len(r.Dups) != 1 || r.Dups[0].Kept.ID != "keep" {
		t.Fatalf("added %d, dups %+v", r.Added, r.Dups)
	}
	if k := find(r.Tree.Root, "Bookmarks Bar").Children[0]; k.ID != "keep" || k.Added != 500 || k.Title != "Go" {
		t.Fatalf("an existing bookmark was changed: %+v", k)
	}
	x := r.Tree.Trash[0]
	if !strings.HasPrefix(x.Reason, DuplicateOf+"keep") || x.At != 42 || x.Node.URL != "https://www.go.dev" {
		t.Fatalf("trash %+v", x)
	}
	if from := find(r.Tree.Root, "Bookmarks Bar", "Deep", "Deeper"); from == nil || x.From != from.ID {
		t.Fatal("the copy's folder must exist, so put back has somewhere to go")
	}
	if base.Root.Children[0].Children[0] != base.Root.Children[0].Children[0] || len(base.Trash) != 0 {
		t.Fatal("base was changed")
	}
}

func TestDeepestCopyWinsAndTakesTheEarliestDateAndATitle(t *testing.T) {
	a := tree(bm("a1", "https://x.example/p", "https://x.example/p", 300))
	b := tree(folder("f", "Other", folder("g", "Reading", bm("b1", "", "https://x.example/p/", 900))))
	c := tree(folder("f", "Other", bm("c1", "X article", "https://x.example/p?utm_medium=mail", 200)))
	r, err := Merge(store.Tree{}, []Source{{"a", a}, {"b", b}, {"c", c}}, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Added != 1 || len(r.Dups) != 2 {
		t.Fatalf("added %d dups %d", r.Added, len(r.Dups))
	}
	kept := find(r.Tree.Root, "Other", "Reading").Children[0]
	if kept.URL != "https://x.example/p/" || kept.Added != 200 || kept.Title != "X article" {
		t.Fatalf("kept %+v", kept)
	}
	if !r.Dups[0].TookDate || !r.Dups[0].TookTitle || r.Dups[0].KeptIn != "Other/Reading" {
		t.Fatalf("dup %+v", r.Dups[0])
	}
	if err := r.Tree.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSeparateKeepsEachSourceInItsOwnFolder(t *testing.T) {
	a := tree(folder("bar", "Bookmarks Bar", bm("1", "A", "https://a.example/", 0)))
	b := tree(folder("bar", "Bookmarks Bar", bm("1", "B", "https://b.example/", 0)))
	r, err := Merge(store.Tree{}, []Source{{"Chrome / work", a}, {"Firefox", b}}, true, 1)
	if err != nil {
		t.Fatal(err)
	}
	if find(r.Tree.Root, "Chrome / work", "Bookmarks Bar") == nil || find(r.Tree.Root, "Firefox", "Bookmarks Bar") == nil {
		t.Fatalf("folders: %+v", r.Tree.Root.Children)
	}
	m, _ := Merge(store.Tree{}, []Source{{"a", a}, {"b", b}}, false, 1)
	if len(m.Tree.Root.Children) != 1 || len(m.Tree.Root.Children[0].Children) != 2 {
		t.Fatal("without separate, folders with the same path are one folder")
	}
}

func TestOldCopiesAreNeverDemotedAndIDsNeverCollide(t *testing.T) {
	base := tree(bm("x1", "A", "https://a.example/", 0), bm("x2", "A again", "https://a.example", 0))
	src := tree(bm("x1", "A", "https://a.example/", 0), bm("x2", "New", "https://new.example/", 0))
	r, err := Merge(base, []Source{{"s", src}}, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tree.Root.Children) != 3 || r.Added != 1 || len(r.Dups) != 1 {
		t.Fatalf("children %d added %d dups %d", len(r.Tree.Root.Children), r.Added, len(r.Dups))
	}
}

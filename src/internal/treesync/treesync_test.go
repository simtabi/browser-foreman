package treesync

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// t3 builds a tree from a compact form: "bar[a,b,x[c]],other[d]" — a name is a bookmark titled and addressed after
// it; name[...] is a folder.
func t3(spec string) store.Tree {
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	stack := []*store.Node{root}
	name := ""
	flush := func() {
		if name == "" {
			return
		}
		p := stack[len(stack)-1]
		p.Children = append(p.Children, &store.Node{ID: name, Kind: store.KindBookmark, Title: name, URL: "https://" + name + ".example/"})
		name = ""
	}
	for _, r := range spec {
		switch r {
		case '[':
			f := &store.Node{ID: name, Kind: store.KindFolder, Title: name}
			stack[len(stack)-1].Children = append(stack[len(stack)-1].Children, f)
			stack, name = append(stack, f), ""
		case ']':
			flush()
			stack = stack[:len(stack)-1]
		case ',':
			flush()
		default:
			name += string(r)
		}
	}
	flush()
	return store.Tree{Root: root}
}

// shape writes a tree back in t3's form, titles in place of ids where they differ.
func shape(n *store.Node) string {
	var parts []string
	for _, c := range n.Children {
		s := c.ID
		if c.Title != c.ID {
			s += "=" + c.Title
		}
		if c.Kind == store.KindFolder {
			s += "[" + shape(c) + "]"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ",")
}

func node(t store.Tree, id string) *store.Node {
	var f func(n *store.Node) *store.Node
	f = func(n *store.Node) *store.Node {
		for _, c := range n.Children {
			if c.ID == id {
				return c
			}
			if x := f(c); x != nil {
				return x
			}
		}
		return nil
	}
	return f(t.Root)
}

func TestMerge3(t *testing.T) {
	base := "bar[a,b,c,x[]],other[d,e,f[g,h]]"
	for _, c := range []struct {
		name      string
		ws, br    func(store.Tree) store.Tree
		newer     Side
		want      string
		conflicts int
		removed   int
	}{
		{"each side's own change", func(t store.Tree) store.Tree { node(t, "a").Title = "A"; return t },
			func(t store.Tree) store.Tree {
				t.Root.Children[1].Children = append(t.Root.Children[1].Children, &store.Node{ID: "n", Kind: store.KindBookmark, Title: "n"})
				return t
			},
			Browser, "bar[a=A,b,c,x[]],other[d,e,f[g,h],n]", 0, 0},
		{"both renamed: the newer wins", func(t store.Tree) store.Tree { node(t, "b").Title = "ws"; return t },
			func(t store.Tree) store.Tree { node(t, "b").Title = "br"; return t },
			Browser, "bar[a,b=br,c,x[]],other[d,e,f[g,h]]", 1, 0},
		{"both renamed, workspace newer", func(t store.Tree) store.Tree { node(t, "b").Title = "ws"; return t },
			func(t store.Tree) store.Tree { node(t, "b").Title = "br"; return t },
			Workspace, "bar[a,b=ws,c,x[]],other[d,e,f[g,h]]", 1, 0},
		{"removed in the browser: to the Trash", unchanged,
			func(t store.Tree) store.Tree { t.Root.Children[1].Children = t.Root.Children[1].Children[1:]; return t },
			Browser, "bar[a,b,c,x[]],other[e,f[g,h]]", 0, 1},
		{"removed in the browser, edited in the workspace after", func(t store.Tree) store.Tree { node(t, "d").Title = "kept"; return t },
			func(t store.Tree) store.Tree { t.Root.Children[1].Children = t.Root.Children[1].Children[1:]; return t },
			Workspace, "bar[a,b,c,x[]],other[d=kept,e,f[g,h]]", 1, 0},
		{"removed in the browser after an edit in the workspace", func(t store.Tree) store.Tree { node(t, "d").Title = "edited"; return t },
			func(t store.Tree) store.Tree { t.Root.Children[1].Children = t.Root.Children[1].Children[1:]; return t },
			Browser, "bar[a,b,c,x[]],other[e,f[g,h]]", 1, 1},
		{"removed in the workspace, edited in the browser after", func(t store.Tree) store.Tree { t.Root.Children[0].Children = t.Root.Children[0].Children[1:]; return t },
			func(t store.Tree) store.Tree { node(t, "a").Title = "back"; return t },
			Browser, "bar[a=back,b,c,x[]],other[d,e,f[g,h]]", 1, 0},
		{"removed in the workspace", func(t store.Tree) store.Tree { t.Root.Children[0].Children = t.Root.Children[0].Children[1:]; return t },
			unchanged, Browser, "bar[b,c,x[]],other[d,e,f[g,h]]", 0, 0},
		{"a folder removed with what it holds", unchanged,
			func(t store.Tree) store.Tree { t.Root.Children[1].Children = t.Root.Children[1].Children[:2]; return t },
			Browser, "bar[a,b,c,x[]],other[d,e]", 0, 3},
		{"moved in the browser", unchanged,
			func(t store.Tree) store.Tree {
				bar := t.Root.Children[0]
				c := bar.Children[2]
				bar.Children = append(bar.Children[:2], bar.Children[3:]...)
				bar.Children[2].Children = []*store.Node{c}
				return t
			}, Workspace, "bar[a,b,x[c]],other[d,e,f[g,h]]", 0, 0},
		{"reordered in the browser, added in the workspace", func(t store.Tree) store.Tree {
			bar := t.Root.Children[0]
			bar.Children = append(bar.Children, &store.Node{ID: "w", Kind: store.KindBookmark, Title: "w"})
			return t
		}, func(t store.Tree) store.Tree {
			bar := t.Root.Children[0]
			bar.Children[0], bar.Children[2] = bar.Children[2], bar.Children[0]
			return t
		}, Workspace, "bar[c,b,a,x[],w],other[d,e,f[g,h]]", 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := Merge3(t3(base), c.ws(t3(base)), c.br(t3(base)), c.newer, 1, "removed in Chrome")
			if got := shape(res.Tree.Root); got != c.want || res.Conflicts != c.conflicts || res.Removed != c.removed {
				t.Fatalf("got  %s (conflicts %d, removed %d)\nwant %s (conflicts %d, removed %d)", got, res.Conflicts, res.Removed, c.want, c.conflicts, c.removed)
			}
			if c.removed > 0 {
				last := res.Tree.Trash[len(res.Tree.Trash)-1]
				if last.Reason != "removed in Chrome" || last.At != 1 || last.From != "other" {
					t.Fatalf("trash %+v", last)
				}
			}
			if err := res.Tree.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func unchanged(t store.Tree) store.Tree { return t }

// What only the workspace knows (a note, a rating, tags) survives a rename made in the browser.
func TestTheWorkspacesOwnFieldsStay(t *testing.T) {
	base := t3("bar[a]")
	ws := t3("bar[a]")
	a := node(ws, "a")
	a.Note, a.Rating, a.Tags = "read later", 4, []string{"go"}
	br := t3("bar[a]")
	node(br, "a").Title = "Renamed"
	got := node(Merge3(base, ws, br, Browser, 1, "x").Tree, "a")
	b, _ := json.Marshal(got)
	if got.Title != "Renamed" || got.Note != "read later" || got.Rating != 4 || len(got.Tags) != 1 {
		t.Fatalf("%s", b)
	}
}

// After the merge is written into the browser, the browser has new ids for what it made, folders and what is in them;
// renaming the head to those ids keeps every note, rating and tag, and anything the workspace changed meanwhile.
func TestNewIDsFollowWhatTheBrowserMade(t *testing.T) {
	written := store.Tree{Root: fo("root", "", fo("ext-1", "Bar", bk("ext-2", "a"), fo("w1", "New", bk("w2", "x"))))}
	read := store.Tree{Root: fo("root", "", fo("ext-1", "Bar", bk("ext-2", "a"), fo("ext-50", "New", bk("ext-51", "x")), bk("ext-60", "made meanwhile")))}
	node(read, "ext-51").URL = node(written, "w2").URL // the same page, under the browser's new id
	m := NewIDs(written, read)
	if len(m) != 2 || m["w1"] != "ext-50" || m["w2"] != "ext-51" {
		t.Fatalf("%v", m)
	}
	head := store.Tree{Root: fo("root", "", fo("ext-1", "Bar", bk("ext-2", "a renamed meanwhile"), fo("w1", "New", bk("w2", "x"))))}
	node(head, "w2").Note, node(head, "w2").Tags = "keep me", []string{"t"}
	head.Trash = []*store.Trashed{{Node: bk("old", "old"), From: "w1"}}
	got := Rename(head, m)
	x := node(got, "ext-51")
	if x == nil || x.Note != "keep me" || len(x.Tags) != 1 || node(got, "ext-2").Title != "a renamed meanwhile" || got.Trash[0].From != "ext-50" {
		t.Fatalf("%s %+v", shape(got.Root), got.Trash[0])
	}
	if node(head, "w2") == nil {
		t.Fatal("Rename changed the tree it was given")
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

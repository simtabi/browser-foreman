package merge

import (
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Change is one bookmark Update would touch.
type Change struct {
	ID       string `json:"id,omitempty"` // the workspace's bookmark, when it has one
	Title    string `json:"title"`
	URL      string `json:"url"`
	Folder   string `json:"folder,omitempty"`
	NewTitle string `json:"new_title,omitempty"`
	Source   string `json:"source"`
}

// Plan is what reading the sources again found: bookmarks new upstream (to add), titles changed upstream on bookmarks
// not renamed here (to take), and bookmarks no longer upstream (reported; they stay).
type Plan struct {
	New     []Change `json:"new"`
	Retitle []Change `json:"retitle"`
	Gone    []Change `json:"gone"`
}

// Upstream is one source as it was last read and as it is now.
type Upstream struct {
	Name     string
	Before   store.Tree
	Now      store.Tree
	Separate bool // added with its folders under a folder named after it (Name), as Merge does
}

type entry struct {
	n    *store.Node
	path string
}

func index(root *store.Node) map[string]entry {
	out := map[string]entry{}
	var walk func(n *store.Node, path string)
	walk = func(n *store.Node, path string) {
		for _, c := range n.Children {
			if c.Kind == store.KindFolder {
				walk(c, join(path, c.Title))
			} else if _, ok := out[Key(c.URL)]; !ok {
				out[Key(c.URL)] = entry{c, path}
			}
		}
	}
	if root != nil {
		walk(root, "")
	}
	return out
}

// PlanUpdate compares each source as it was with how it is now, against the workspace.
func PlanUpdate(ws store.Tree, ups []Upstream) Plan {
	have := index(ws.Root)
	for _, x := range ws.Trash { // a page in the Trash (a copy, or one thrown away) is not new
		if x.Node.Kind == store.KindFolder {
			for k, e := range index(&store.Node{Kind: store.KindFolder, Children: []*store.Node{x.Node}}) {
				if _, ok := have[k]; !ok {
					have[k] = entry{e.n, "\x00trash"}
				}
			}
		} else if _, ok := have[Key(x.Node.URL)]; !ok {
			have[Key(x.Node.URL)] = entry{x.Node, "\x00trash"}
		}
	}
	p := Plan{New: []Change{}, Retitle: []Change{}, Gone: []Change{}}
	seenNew := map[string]bool{}
	for _, u := range ups {
		before, now := index(u.Before.Root), index(u.Now.Root)
		for k, e := range now {
			mine, ok := have[k]
			switch {
			case !ok && !seenNew[k]:
				seenNew[k] = true
				p.New = append(p.New, Change{Title: e.n.Title, URL: e.n.URL, Folder: Display(e.path), Source: u.Name})
			case ok && mine.path != "\x00trash":
				old, was := before[k]
				t := strings.TrimSpace(e.n.Title)
				if was && t != "" && t != strings.TrimSpace(old.n.Title) && mine.n.Title == old.n.Title {
					p.Retitle = append(p.Retitle, Change{ID: mine.n.ID, Title: mine.n.Title, URL: mine.n.URL, Folder: Display(mine.path), NewTitle: t, Source: u.Name})
				}
			}
		}
		for k := range before {
			if _, still := now[k]; still {
				continue
			}
			if mine, ok := have[k]; ok && mine.path != "\x00trash" {
				p.Gone = append(p.Gone, Change{ID: mine.n.ID, Title: mine.n.Title, URL: mine.n.URL, Folder: Display(mine.path), Source: u.Name})
			}
		}
	}
	return p
}

// ApplyUpdate adds what is new (by Merge, so copies among the new ones go to the Trash) and takes the new titles.
// Nothing is removed.
func ApplyUpdate(ws store.Tree, ups []Upstream, p Plan, now int64) (Result, error) {
	retitle := map[string]string{}
	for _, c := range p.Retitle {
		retitle[c.ID] = c.NewTitle
	}
	want := map[string]bool{}
	for _, c := range p.New {
		want[Key(c.URL)] = true
	}
	var sources []Source
	separate := false
	for _, u := range ups {
		root := prune(u.Now.Root, want)
		if root == nil {
			continue
		}
		sources = append(sources, Source{Name: u.Name, Tree: store.Tree{Root: root}})
		separate = separate || u.Separate
	}
	res, err := Merge(ws, sources, separate, now)
	if err != nil {
		return res, err
	}
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		if t, ok := retitle[n.ID]; ok && n.Kind == store.KindBookmark {
			n.Title = t
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(res.Tree.Root)
	return res, nil
}

// prune keeps only the bookmarks whose key is wanted, and the folders leading to them; nil when none is left.
func prune(n *store.Node, want map[string]bool) *store.Node {
	if n == nil {
		return nil
	}
	out := *n
	out.Children = nil
	for _, c := range n.Children {
		if c.Kind == store.KindFolder {
			if k := prune(c, want); k != nil {
				out.Children = append(out.Children, k)
			}
		} else if want[Key(c.URL)] {
			out.Children = append(out.Children, c)
		}
	}
	if len(out.Children) == 0 {
		return nil
	}
	return &out
}

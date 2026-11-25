// Package treesync merges two copies of one bookmark tree that changed apart, a workspace and the browser it was read
// from, against the tree they last agreed on. Bookmarks are matched by id (a workspace read from a browser keeps the
// browser's ids). A change made on one side only is kept; where both sides changed the same thing, the side that
// changed last wins, and that holds for a removal against an edit too. What the merge removes from the workspace goes
// to its Trash.
package treesync

import (
	"slices"
	"sort"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Side names which copy changed last, for the conflicts.
type Side int

const (
	Workspace Side = iota
	Browser
)

// Result is the merged tree and what the merge did.
type Result struct {
	Tree      store.Tree
	Conflicts int // places both sides changed differently, settled for the newer side
	Removed   int // bookmarks and folders removed from the workspace (now in its Trash)
}

// place is where a node sits and what it says, as the merge compares it.
type place struct {
	node   *store.Node
	parent string
	index  int
}

func index(t store.Tree) map[string]place {
	out := map[string]place{}
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		for i, c := range n.Children {
			out[c.ID] = place{c, n.ID, i}
			walk(c)
		}
	}
	if t.Root != nil {
		walk(t.Root)
	}
	return out
}

// Merge3 merges ws (the workspace) and br (the browser) against base, the tree they last agreed on. newer says which
// side changed last. at stamps what goes to the Trash; reason says why.
func Merge3(base, ws, br store.Tree, newer Side, at int64, reason string) Result {
	b, w, r := index(base), index(ws), index(br)
	ids := map[string]bool{}
	for _, m := range []map[string]place{b, w, r} {
		for id := range m {
			ids[id] = true
		}
	}
	var res Result
	type out struct {
		node   *store.Node
		parent string
	}
	kept := map[string]out{}
	var removed []place
	for id := range ids {
		bp, inB := b[id]
		wp, inW := w[id]
		rp, inR := r[id]
		switch {
		case !inB && inW && inR: // added on both sides with one id: the newer one
			res.Conflicts++
			if newer == Browser {
				kept[id] = out{copyNode(rp.node, wp.node), rp.parent}
			} else {
				kept[id] = out{copyNode(wp.node, nil), wp.parent}
			}
		case !inB && inW:
			kept[id] = out{copyNode(wp.node, nil), wp.parent}
		case !inB && inR:
			kept[id] = out{copyNode(rp.node, nil), rp.parent}
		case inB && !inW && !inR: // removed on both
		case inB && !inW: // removed in the workspace
			if same(bp, rp) || newer == Workspace {
				if !same(bp, rp) {
					res.Conflicts++
				}
				continue
			}
			res.Conflicts++ // edited in the browser after: it comes back
			kept[id] = out{copyNode(rp.node, bp.node), rp.parent}
		case inB && !inR: // removed in the browser
			if same(bp, wp) || newer == Browser {
				if !same(bp, wp) {
					res.Conflicts++
				}
				removed = append(removed, wp)
				continue
			}
			res.Conflicts++ // edited in the workspace after: it stays
			kept[id] = out{copyNode(wp.node, nil), wp.parent}
		default: // on all three: field by field
			n := copyNode(wp.node, nil)
			pick := func(base, ws, br string) string {
				switch {
				case ws == br || br == base:
					return ws
				case ws == base:
					return br
				}
				res.Conflicts++
				if newer == Browser {
					return br
				}
				return ws
			}
			n.Title = pick(bp.node.Title, wp.node.Title, rp.node.Title)
			n.URL = pick(bp.node.URL, wp.node.URL, rp.node.URL)
			kept[id] = out{n, pick(bp.parent, wp.parent, rp.parent)}
		}
	}
	// Build the tree: each kept node under its parent, in index order. A node whose parent is gone goes to the nearest
	// kept folder above it in base (or the workspace), else to the first top folder.
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	if ws.Root != nil {
		root.Title = ws.Root.Title
	}
	children := map[string][]string{}
	for id, o := range kept {
		p := o.parent
		for p != "root" && p != "" {
			if _, ok := kept[p]; ok {
				break
			}
			if bp, ok := b[p]; ok {
				p = bp.parent
			} else if wp, ok := w[p]; ok {
				p = wp.parent
			} else if rp, ok := r[p]; ok {
				p = rp.parent
			} else {
				p = "root"
			}
		}
		if p == "" {
			p = "root"
		}
		children[p] = append(children[p], id)
	}
	var attach func(n *store.Node)
	attach = func(n *store.Node) {
		ids := children[n.ID]
		ordered(ids, n.ID, base, ws, br, newer)
		for _, id := range ids {
			c := kept[id].node
			n.Children = append(n.Children, c)
			if c.Kind == store.KindFolder {
				attach(c)
			}
		}
	}
	attach(root)
	// What went: only the topmost of each removed branch goes to the Trash, holding what was under it.
	trash := slices.Clone(ws.Trash)
	gone := map[string]bool{}
	for _, p := range removed {
		gone[p.node.ID] = true
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].node.ID < removed[j].node.ID })
	for _, p := range removed {
		if gone[p.parent] {
			continue
		}
		trash = append(trash, &store.Trashed{Node: p.node, From: p.parent, Index: p.index, At: at, Reason: reason})
		res.Removed += count(p.node)
	}
	res.Tree = store.Tree{Root: root, Trash: trash}
	return res
}

// same reports whether a node is where and what it was: its folder, title and address. Its position is left out: it
// shifts whenever something before it comes or goes.
func same(a, b place) bool {
	return a.parent == b.parent && a.node.Title == b.node.Title && a.node.URL == b.node.URL
}

// ordered sorts ids, the merged contents of folder parent, by the order of the side that reordered that folder's
// existing contents (the newer side, if both did); what that side does not hold keeps its place from the other side.
func ordered(ids []string, parent string, base, ws, br store.Tree, newer Side) {
	list := func(t store.Tree) []string {
		n := find(t.Root, parent)
		if n == nil {
			return nil
		}
		out := make([]string, len(n.Children))
		for i, c := range n.Children {
			out[i] = c.ID
		}
		return out
	}
	b, w, r := list(base), list(ws), list(br)
	reordered := func(side []string) bool {
		in := map[string]bool{}
		for _, id := range b {
			in[id] = true
		}
		var kept, was []string
		for _, id := range side {
			if in[id] {
				kept = append(kept, id)
			}
		}
		on := map[string]bool{}
		for _, id := range side {
			on[id] = true
		}
		for _, id := range b {
			if on[id] {
				was = append(was, id)
			}
		}
		return !slices.Equal(kept, was)
	}
	first, second := w, r
	if rw, rb := reordered(w), reordered(r); rb && (!rw || newer == Browser) {
		first, second = r, w
	}
	pos := func(id string) float64 {
		if i := slices.Index(first, id); i >= 0 {
			return float64(i)
		}
		if i := slices.Index(second, id); i >= 0 {
			// after what comes before it on its own side
			for k := i - 1; k >= 0; k-- {
				if j := slices.Index(first, second[k]); j >= 0 {
					return float64(j) + 0.5
				}
			}
			return -0.5
		}
		return float64(len(first))
	}
	sort.SliceStable(ids, func(i, j int) bool {
		pi, pj := pos(ids[i]), pos(ids[j])
		if pi != pj {
			return pi < pj
		}
		return ids[i] < ids[j]
	})
}

func find(n *store.Node, id string) *store.Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if f := find(c, id); f != nil {
			return f
		}
	}
	return nil
}

// copyNode is n without its children, keeping what only the workspace knows (note, description, rating, tags, added)
// from ws when n came from the browser.
func copyNode(n, ws *store.Node) *store.Node {
	c := *n
	c.Children = nil
	if ws != nil {
		c.Note, c.Description, c.Rating, c.Tags = ws.Note, ws.Description, ws.Rating, ws.Tags
		if c.Added == 0 {
			c.Added = ws.Added
		}
	}
	return &c
}

func count(n *store.Node) int {
	k := 1
	for _, c := range n.Children {
		k += count(c)
	}
	return k
}

// Adopt takes the browser's tree, read back after the merge was written into it, as the workspace's next state: the
// same bookmarks under the browser's ids. What only the workspace knows (note, description, rating, tags) comes along:
// by id, and, for what the browser made anew (it gives a new id to each bookmark written into it), by folder, title
// and address. The workspace's Trash is kept.
func Adopt(head, browser store.Tree) store.Tree {
	h := index(head)
	out := store.Tree{Root: cloneTree(browser.Root), Trash: slices.Clone(head.Trash)}
	r := index(out)
	type key struct{ parent, title, url string }
	spare := map[key][]*store.Node{} // workspace nodes the browser does not hold by id
	for id, p := range h {
		if _, ok := r[id]; !ok {
			k := key{p.parent, p.node.Title, p.node.URL}
			spare[k] = append(spare[k], p.node)
		}
	}
	for _, list := range spare {
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	}
	for id, p := range r {
		from := (*store.Node)(nil)
		if hp, ok := h[id]; ok {
			from = hp.node
		} else {
			k := key{p.parent, p.node.Title, p.node.URL}
			if list := spare[k]; len(list) > 0 {
				from, spare[k] = list[0], list[1:]
			}
		}
		if from != nil {
			p.node.Note, p.node.Description, p.node.Rating, p.node.Tags = from.Note, from.Description, from.Rating, from.Tags
			if p.node.Added == 0 {
				p.node.Added = from.Added
			}
		}
	}
	return out
}

func cloneTree(n *store.Node) *store.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Children = make([]*store.Node, len(n.Children))
	for i, x := range n.Children {
		c.Children[i] = cloneTree(x)
	}
	if len(n.Children) == 0 {
		c.Children = nil
	}
	return &c
}

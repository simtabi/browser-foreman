// Package merge adds bookmarks from other sources (files, other workspaces) to a workspace and folds copies of the
// same page into one. It is the Python tool's rule: a bookmark already in the workspace always wins, so adding a source
// never moves what is being cleaned; among new copies the one in the deepest folder wins, taking the earliest date
// added and a real title when it had none. Every copy that loses goes to the workspace's Trash, as "duplicate of" the
// one kept, where it can be put back: nothing is dropped.
package merge

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// DuplicateOf starts the Trash reason of a merged copy; the kept bookmark's id follows.
const DuplicateOf = "duplicate of "

// tracking are query parameters that never change which page a link opens.
var tracking = []string{"utm_", "fbclid", "gclid", "mc_", "ref_src", "igshid"}

// Key is what two links share when they open the same page: for web links the host in lower case without "www.",
// the path without a trailing slash, the query without tracking parameters, and the fragment unless it only
// highlights text. Anything else is compared as written.
func Key(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return raw
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	path := strings.TrimRight(u.EscapedPath(), "/")
	if path == "" {
		path = "/"
	}
	var keep []string
	for _, kv := range strings.Split(u.RawQuery, "&") {
		if kv == "" {
			continue
		}
		k, _, _ := strings.Cut(kv, "=")
		k, _ = url.QueryUnescape(k)
		lk := strings.ToLower(k)
		if lk == "ref" || slices.ContainsFunc(tracking, func(p string) bool { return strings.HasPrefix(lk, p) }) {
			continue
		}
		keep = append(keep, kv)
	}
	frag := u.EscapedFragment()
	if strings.HasPrefix(u.Fragment, ":~:") {
		frag = ""
	}
	k := "//" + host + path
	if len(keep) > 0 {
		k += "?" + strings.Join(keep, "&")
	}
	if frag != "" {
		k += "#" + frag
	}
	return k
}

// Source is bookmarks to add, named for the history entry and the duplicates list.
type Source struct {
	Name string
	Tree store.Tree // only Root is read; a source's Trash is never added
}

// Dup is one copy folded into another.
type Dup struct {
	Kept      *store.Node
	KeptIn    string // folder path
	Removed   *store.Node
	WasIn     string
	Source    string
	TookDate  bool // the kept bookmark took this copy's earlier date
	TookTitle bool
}

// Result is the merged tree and what happened.
type Result struct {
	Tree   store.Tree
	Added  int // new bookmarks placed
	Dups   []Dup
	Folder int // new folders made
}

type cand struct {
	n      *store.Node
	path   string // folder titles joined by sep
	source string
	old    bool
	order  int
}

// Merge adds sources to base (which it does not change) and folds copies. With separate, each source's folders go
// under a folder named after it; otherwise folders with the same path are one folder. now stamps Trash entries.
func Merge(base store.Tree, sources []Source, separate bool, now int64) (Result, error) {
	if base.Root == nil {
		base.Root = &store.Node{ID: "root", Kind: store.KindFolder}
	}
	out := clone(base)
	used := map[string]bool{}
	var mark func(n *store.Node)
	mark = func(n *store.Node) {
		used[n.ID] = true
		for _, c := range n.Children {
			mark(c)
		}
	}
	mark(out.Root)
	for _, x := range out.Trash {
		mark(x.Node)
	}
	newID := func() string {
		for {
			b := make([]byte, 6)
			_, _ = rand.Read(b)
			id := "m-" + hex.EncodeToString(b)
			if !used[id] {
				used[id] = true
				return id
			}
		}
	}

	// Folders by path, the existing ones first.
	folders := map[string]*store.Node{"": out.Root}
	var cands []*cand
	var index func(n *store.Node, path string)
	index = func(n *store.Node, path string) {
		for _, c := range n.Children {
			if c.Kind == store.KindFolder {
				p := join(path, c.Title)
				if _, ok := folders[p]; !ok {
					folders[p] = c
				}
				index(c, p)
			} else {
				cands = append(cands, &cand{n: c, path: path, old: true, order: len(cands)})
			}
		}
	}
	index(out.Root, "")
	res := Result{}
	var folderAt func(path string) *store.Node
	folderAt = func(path string) *store.Node {
		if f, ok := folders[path]; ok {
			return f
		}
		parentPath, name := "", path
		if i := strings.LastIndex(path, sep); i >= 0 {
			parentPath, name = path[:i], path[i+len(sep):]
		}
		parent := folderAt(parentPath)
		f := &store.Node{ID: newID(), Kind: store.KindFolder, Title: name}
		parent.Children = append(parent.Children, f)
		folders[path] = f
		res.Folder++
		return f
	}

	for _, s := range sources {
		if s.Tree.Root == nil {
			return Result{}, errors.New(s.Name + " has no bookmarks tree")
		}
		pre := ""
		if separate {
			pre = strings.Join(strings.Fields(s.Name), " ")
			if pre == "" {
				pre = "Imported"
			}
		}
		var walk func(n *store.Node, path string)
		walk = func(n *store.Node, path string) {
			for _, c := range n.Children {
				if c.Kind == store.KindFolder {
					p := join(path, c.Title)
					folderAt(p)
					walk(c, p)
					continue
				}
				nb := *c
				nb.ID = newID()
				nb.Children = nil
				cands = append(cands, &cand{n: &nb, path: path, source: s.Name, order: len(cands)})
			}
		}
		if pre != "" {
			folderAt(pre)
		}
		walk(s.Tree.Root, pre)
	}

	groups := map[string][]*cand{}
	var keys []string
	for _, c := range cands {
		k := Key(c.n.URL)
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], c)
	}
	for _, k := range keys {
		g := groups[k]
		slices.SortStableFunc(g, func(a, b *cand) int {
			return cmp.Or(
				boolCmp(!a.old, !b.old),
				boolCmp(a.path == "", b.path == ""),
				cmp.Compare(depth(b.path), depth(a.path)),
				cmp.Compare(orMax(a.n.Added), orMax(b.n.Added)),
				cmp.Compare(a.order, b.order))
		})
		w, losers := g[0], g[1:]
		tookDate, tookTitle := false, false
		if !w.old && len(losers) > 0 {
			for _, l := range losers {
				if l.n.Added != 0 && (w.n.Added == 0 || l.n.Added < w.n.Added) {
					w.n.Added, tookDate = l.n.Added, true
				}
			}
			if t := strings.TrimSpace(w.n.Title); t == "" || t == w.n.URL {
				for _, l := range losers {
					if t := strings.TrimSpace(l.n.Title); t != "" && t != l.n.URL {
						w.n.Title, tookTitle = t, true
						break
					}
				}
			}
		}
		if !w.old {
			f := folderAt(w.path)
			f.Children = append(f.Children, w.n)
			res.Added++
		}
		for _, l := range losers {
			if l.old {
				continue // never demote what is already in the workspace
			}
			f := folderAt(l.path)
			out.Trash = append(out.Trash, &store.Trashed{Node: l.n, From: f.ID, Index: len(f.Children), At: now, Reason: DuplicateOf + w.n.ID})
			res.Dups = append(res.Dups, Dup{Kept: w.n, KeptIn: Display(w.path), Removed: l.n, WasIn: Display(l.path), Source: l.source,
				TookDate: tookDate, TookTitle: tookTitle})
		}
	}
	res.Tree = out
	return res, out.Validate()
}

// sep joins folder titles inside a path, so a title holding "/" stays one folder.
const sep = "\x1f"

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + sep + name
}

func depth(path string) int {
	if path == "" {
		return 0
	}
	return strings.Count(path, sep) + 1
}

// Display is a folder path as people read it: titles joined by "/", as the Python tool showed them.
func Display(path string) string { return strings.ReplaceAll(path, sep, "/") }

func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func orMax(t int64) int64 {
	if t == 0 {
		return 1 << 62
	}
	return t
}

func clone(t store.Tree) store.Tree {
	var cp func(n *store.Node) *store.Node
	cp = func(n *store.Node) *store.Node {
		c := *n
		c.Children = nil
		for _, k := range n.Children {
			c.Children = append(c.Children, cp(k))
		}
		return &c
	}
	out := store.Tree{Root: cp(t.Root)}
	for _, x := range t.Trash {
		y := *x
		y.Node = cp(x.Node)
		out.Trash = append(out.Trash, &y)
	}
	return out
}

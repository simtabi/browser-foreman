// Package writeback plans writing a cleaned workspace back into a browser profile that has moved on since it was
// exported: a three-way merge of the export (base), the cleaned workspace (ours) and the profile now (theirs). The
// target it builds keeps the profile's own ids on everything it matches, so a write moves and renames rather than
// recreating, and Report says exactly what the write would do.
package writeback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/merge"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Mode says what happens to what the profile gained after the export.
type Mode string

const (
	// Merge applies the cleanup and keeps what the profile changed since the export.
	Merge Mode = "merge"
	// Replace makes the profile hold the cleanup; what it gained since the export is dropped, once confirmed.
	Replace Mode = "replace"
)

// The three roots a Chromium profile always has, by role.
const (
	Bar    = "bar"
	Other  = "other"
	Mobile = "mobile"
)

var roles = []string{Bar, Other, Mobile}

// roleOf names the root a top folder of an imported tree stands for ("" when none: its items go to Other).
func roleOf(title string) string {
	switch strings.ToLower(clean(title)) {
	case "bookmarks bar", "bookmarks toolbar", "favorites bar", "favourites bar":
		return Bar
	case "other bookmarks", "other favorites", "other favourites":
		return Other
	case "mobile bookmarks":
		return Mobile
	}
	return ""
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// segment is a folder title as one step of a path: "/" (and "%", "#") inside a title are escaped, so "CI/CD" is one
// folder, not "CI" holding "CD". unsegment undoes it, and drops the "#n" that tells same-named siblings apart.
var segEsc, segUnesc = strings.NewReplacer("%", "%25", "/", "%2F", "#", "%23"), strings.NewReplacer("%2F", "/", "%23", "#", "%25", "%")

func segment(title string) string { return segEsc.Replace(clean(title)) }

func unsegment(seg string) string {
	if i := strings.LastIndex(seg, "#"); i >= 0 {
		seg = seg[:i]
	}
	return segUnesc.Replace(seg)
}

// Item is one bookmark or folder in the report, by where it is in the profile.
type Item struct {
	ID       string `json:"id,omitempty"`
	Title    string `json:"title"`
	URL      string `json:"url,omitempty"`
	Folder   string `json:"folder"`
	To       string `json:"to,omitempty"`      // a move's new folder
	Was      string `json:"was,omitempty"`     // a rename's old title
	WasURL   string `json:"was_url,omitempty"` // a changed address's old one
	IsFolder bool   `json:"is_folder,omitempty"`
}

// Report is what writing the target would do to the profile, and why.
type Report struct {
	Mode Mode `json:"mode"`
	// What the write changes in the profile. Renamed holds new titles and new addresses (WasURL set).
	Added   []Item `json:"added"`
	Removed []Item `json:"removed"`
	Moved   []Item `json:"moved"`
	Renamed []Item `json:"renamed"`
	// What the profile changed after the export. Merge keeps these; replace drops the additions (DropNew).
	AddedSince   []Item `json:"added_since"`
	RemovedSince []Item `json:"removed_since"`
	MovedSince   []Item `json:"moved_since"`
	RenamedSince []Item `json:"renamed_since"`
	// Bookmarks both sides changed differently; the cleanup won.
	Conflicts []Item `json:"conflicts"`
	// DropNew is how many bookmarks replace removes that the profile gained after the export: applying a replace needs
	// this number confirmed.
	DropNew int `json:"drop_new"`
	// NotWritten counts what the profile's format has no place for (notes, descriptions, ratings, tags): it stays in
	// Browser Foreman.
	NotWritten int `json:"not_written"`
	// Fingerprint is theirs as planned against: a write refuses a profile that changed since.
	Fingerprint string `json:"fingerprint"`
}

// Empty says whether writing the target changes nothing.
func (r Report) Empty() bool {
	return len(r.Added)+len(r.Removed)+len(r.Moved)+len(r.Renamed) == 0
}

// node is a bookmark or folder in a tree laid out by role.
type node struct {
	n      *store.Node
	parent *node
	path   string // role, then normalised folder titles, joined by "/"
	key    string // bookmarks: the address key and its occurrence ("…#2")
	kids   []*node
}

// laid is a tree laid out by role: its three roots, its bookmarks by key and its folders by path.
type laid struct {
	roots   map[string]*node
	marks   map[string]*node
	folders map[string]*node
	order   []*node // bookmarks in document order
}

// layout reads t by role. With byPosition (a profile's own tree), the top folders are the roots in Chromium's order:
// bar, other, mobile; otherwise they are recognised by title, and anything else at the top belongs to Other.
func layout(t store.Tree, byPosition bool) (*laid, error) {
	if t.Root == nil {
		return nil, errors.New("an empty tree")
	}
	l := &laid{roots: map[string]*node{}, marks: map[string]*node{}, folders: map[string]*node{}}
	seen := map[string]int{}
	same := map[string]int{} // folders seen per path: the second "Work" beside the first is Work#2
	var walk func(p *node, kids []*store.Node)
	walk = func(p *node, kids []*store.Node) {
		for _, c := range kids {
			x := &node{n: c, parent: p}
			p.kids = append(p.kids, x)
			if c.Kind == store.KindFolder {
				x.path = p.path + "/" + segment(c.Title)
				if same[x.path]++; same[x.path] > 1 {
					x.path += "#" + strconv.Itoa(same[x.path])
				}
				l.folders[x.path] = x
				walk(x, c.Children)
				continue
			}
			k := merge.Key(c.URL)
			seen[k]++
			x.key = fmt.Sprintf("%s#%d", k, seen[k])
			l.marks[x.key] = x
			l.order = append(l.order, x)
		}
	}
	for _, r := range roles {
		l.roots[r] = &node{n: &store.Node{Kind: store.KindFolder}, path: r}
		l.folders[r] = l.roots[r]
	}
	var loose []*store.Node
	for i, top := range t.Root.Children {
		role := ""
		switch {
		case byPosition && top.Kind == store.KindFolder && i < len(roles):
			role = roles[i]
		case !byPosition && top.Kind == store.KindFolder:
			role = roleOf(top.Title)
		}
		if role == "" {
			loose = append(loose, top)
			continue
		}
		l.roots[role].n = top
		walk(l.roots[role], top.Children)
	}
	walk(l.roots[Other], loose)
	return l, nil
}

// Plan builds the tree theirs should hold. base is the export the cleanup started from, ours the cleaned workspace,
// theirs the profile now (its top folders in Chromium's order: bar, other, mobile). The target keeps theirs' ids on
// every node it matches; Mobile bookmarks is kept as it is.
func Plan(base, ours, theirs store.Tree, mode Mode) (store.Tree, Report, error) {
	if mode != Merge && mode != Replace {
		return store.Tree{}, Report{}, fmt.Errorf("mode is merge or replace, not %q", mode)
	}
	b, err := layout(base, false)
	if err != nil {
		return store.Tree{}, Report{}, fmt.Errorf("base: %w", err)
	}
	o, err := layout(ours, false)
	if err != nil {
		return store.Tree{}, Report{}, fmt.Errorf("ours: %w", err)
	}
	th, err := layout(theirs, true)
	if err != nil {
		return store.Tree{}, Report{}, fmt.Errorf("theirs: %w", err)
	}
	rep := Report{Mode: mode, Fingerprint: Fingerprint(theirs)}

	// The target starts as ours, laid out by role, with Mobile bookmarks taken from theirs.
	t := map[string]*store.Node{}
	for _, r := range roles {
		src := o.roots[r].n
		if r == Mobile {
			src = th.roots[Mobile].n
		}
		t[r] = copyNode(src, &rep, r != Mobile)
	}
	tl, _ := layout(store.Tree{Root: &store.Node{Kind: store.KindFolder, Children: []*store.Node{t[Bar], t[Other], t[Mobile]}}}, true)

	// What the profile did after the export.
	for _, k := range keysOf(b.order) {
		bx, tx, ox := b.marks[k], th.marks[k], o.marks[k]
		// A change counts as the profile's only where the profile differs from the cleanup as well as from the export:
		// what the cleanup did and the profile already holds (after a write) is not the profile's own change.
		if tx == nil {
			if ox != nil {
				rep.RemovedSince = append(rep.RemovedSince, item(bx))
			}
			if x := tl.marks[k]; x != nil {
				detach(x) // deleted in the profile since the export: never brought back
			}
			continue
		}
		if place(tx) != place(bx) && (ox == nil || place(tx) != place(ox)) {
			rep.MovedSince = append(rep.MovedSince, moved(bx, tx))
		}
		if clean(tx.n.Title) != clean(bx.n.Title) && (ox == nil || clean(tx.n.Title) != clean(ox.n.Title)) {
			rep.RenamedSince = append(rep.RenamedSince, renamed(tx, bx.n.Title))
		}
		x := tl.marks[k]
		if x == nil || mode == Replace {
			continue
		}
		oursMoved := o.marks[k] != nil && place(o.marks[k]) != place(bx)
		oursRenamed := o.marks[k] != nil && clean(o.marks[k].n.Title) != clean(bx.n.Title)
		if place(tx) != place(bx) {
			if oursMoved && place(tx) == place(o.marks[k]) {
				// both moved it to the same folder: nothing to decide
			} else if oursMoved {
				rep.Conflicts = append(rep.Conflicts, item(x))
			} else {
				move(x, ensure(tl, tx.parent.path, th))
			}
		}
		if clean(tx.n.Title) != clean(bx.n.Title) {
			if oursRenamed && clean(o.marks[k].n.Title) != clean(tx.n.Title) {
				rep.Conflicts = append(rep.Conflicts, item(x))
			} else {
				x.n.Title = tx.n.Title
			}
		}
	}
	for _, tx := range th.order {
		if b.marks[tx.key] != nil || tl.marks[tx.key] != nil || strings.HasPrefix(tx.parent.path, Mobile) {
			continue
		}
		rep.AddedSince = append(rep.AddedSince, item(tx))
		if mode == Replace {
			rep.DropNew++
			continue
		}
		f := ensure(tl, tx.parent.path, th)
		c := &store.Node{ID: tx.n.ID, Kind: store.KindBookmark, Title: tx.n.Title, URL: tx.n.URL, Added: tx.n.Added}
		x := &node{n: c, parent: f, key: tx.key}
		f.n.Children = append(f.n.Children, c)
		f.kids = append(f.kids, x)
		tl.marks[tx.key] = x
	}

	// Ids: theirs' for every match (roots by role, folders by path, bookmarks by key); anything else is new.
	used := map[string]bool{}
	for _, r := range roles {
		t[r].ID, t[r].Title = th.roots[r].n.ID, th.roots[r].n.Title
		used[t[r].ID] = true
	}
	folders := matchFolders(tl, th, used)
	n := 0
	var assign func(x *node)
	assign = func(x *node) {
		for _, c := range x.kids {
			c.n.ID = ""
			if c.n.Kind == store.KindBookmark {
				if y := th.marks[c.key]; y != nil && !used[y.n.ID] {
					c.n.ID = y.n.ID
					used[y.n.ID] = true
					if clean(c.n.Title) == clean(y.n.Title) {
						c.n.Title = y.n.Title // only whitespace differs: the profile's own title stays
					}
				}
			} else if y := folders[c]; y != nil {
				c.n.ID = y.n.ID
				if clean(c.n.Title) == clean(y.n.Title) {
					c.n.Title = y.n.Title // only whitespace differs: the profile's own title stays
				}
			}
			assign(c)
		}
	}
	assign(tl.roots[Bar])
	assign(tl.roots[Other])
	// A bookmark whose address the cleanup changed (a link that moved) takes the one bookmark of theirs left over
	// with its title, so the write changes its address rather than removing it and adding a copy.
	left := map[string][]*node{}
	for _, y := range th.order {
		if !used[y.n.ID] && !strings.HasPrefix(y.parent.path, Mobile) {
			left[clean(y.n.Title)] = append(left[clean(y.n.Title)], y)
		}
	}
	var fill func(x *node)
	fill = func(x *node) {
		for _, c := range x.kids {
			if c.n.ID == "" && c.n.Kind == store.KindBookmark {
				if ys := left[clean(c.n.Title)]; len(ys) == 1 && !used[ys[0].n.ID] && c.n.Title != "" {
					c.n.ID = ys[0].n.ID
					used[c.n.ID] = true
				}
			}
			if c.n.ID == "" {
				n++
				c.n.ID = fmt.Sprintf("new-%d", n)
			}
			fill(c)
		}
	}
	fill(tl.roots[Bar])
	fill(tl.roots[Other])

	top := []*store.Node{t[Bar], t[Other]}
	if th.roots[Mobile].n.ID != "" || len(t[Mobile].Children) > 0 {
		top = append(top, t[Mobile])
	}
	target := store.Tree{Root: &store.Node{ID: theirs.Root.ID, Kind: store.KindFolder, Title: theirs.Root.Title, Children: top}}
	diff(&rep, theirs, target)
	return target, rep, nil
}

// browserURL is an address as a browser stores it: a default port (443 for https, 80 for http) dropped, so a write
// never sets an address the browser then changes back (which would read as a change on every later plan).
func browserURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Port() == "" || !((u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80")) {
		return raw
	}
	u.Host = u.Hostname()
	if strings.Contains(u.Host, ":") { // an IPv6 address keeps its brackets
		u.Host = "[" + u.Host + "]"
	}
	return u.String()
}

// copyNode copies a subtree, counting what a browser cannot hold. Ids are kept for now; Plan replaces them.
func copyNode(n *store.Node, rep *Report, count bool) *store.Node {
	c := &store.Node{ID: n.ID, Kind: n.Kind, Title: n.Title, URL: browserURL(n.URL), Added: n.Added}
	if c.Kind == "" {
		c.Kind = store.KindFolder
	}
	if count && (n.Note != "" || n.Description != "" || n.Rating != 0 || len(n.Tags) > 0) {
		rep.NotWritten++
	}
	for _, k := range n.Children {
		c.Children = append(c.Children, copyNode(k, rep, count))
	}
	return c
}

func keysOf(xs []*node) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = x.key
	}
	return out
}

func detach(x *node) {
	p := x.parent
	p.n.Children = slices.DeleteFunc(p.n.Children, func(c *store.Node) bool { return c == x.n })
	p.kids = slices.DeleteFunc(p.kids, func(c *node) bool { return c == x })
}

func move(x *node, to *node) {
	if x.parent == to {
		return
	}
	detach(x)
	x.parent = to
	to.n.Children = append(to.n.Children, x.n)
	to.kids = append(to.kids, x)
}

// ensure returns the target's folder at path, making the missing folders (named as in theirs) along the way.
func ensure(tl *laid, path string, th *laid) *node {
	if f := tl.folders[path]; f != nil {
		return f
	}
	i := strings.LastIndex(path, "/")
	parent := ensure(tl, path[:i], th)
	title := unsegment(path[i+1:])
	if y := th.folders[path]; y != nil {
		title = y.n.Title
	}
	c := &store.Node{Kind: store.KindFolder, Title: title}
	x := &node{n: c, parent: parent, path: path}
	parent.n.Children = append(parent.n.Children, c)
	parent.kids = append(parent.kids, x)
	tl.folders[path] = x
	return x
}

// place is the folder a bookmark sits in, by name (a root by its role): a bookmark whose folder moved has not moved.
func place(x *node) string { return x.parent.path[strings.LastIndex(x.parent.path, "/")+1:] }

// matchFolders pairs the target's folders with theirs, parents first: by path; else, with the same name, the folder
// holding most of its bookmarks in theirs (the cleanup moved it); else the one unused folder of theirs with that name.
func matchFolders(tl, th *laid, used map[string]bool) map[*node]*node {
	out := map[*node]*node{}
	byName := map[string][]*node{}
	for _, p := range sortedKeys(th.folders) {
		if y := th.folders[p]; !slices.Contains(roles, p) {
			name := clean(y.n.Title)
			byName[name] = append(byName[name], y)
		}
	}
	take := func(x, y *node) bool {
		if y == nil || used[y.n.ID] || clean(y.n.Title) != clean(x.n.Title) {
			return false
		}
		out[x], used[y.n.ID] = y, true
		return true
	}
	var walk func(x *node)
	walk = func(x *node) {
		for _, c := range x.kids {
			if c.n.Kind != store.KindFolder {
				continue
			}
			if !take(c, th.folders[c.path]) {
				votes := map[*node]int{}
				var best *node
				for _, k := range c.kids {
					if y := th.marks[k.key]; k.n.Kind == store.KindBookmark && y != nil && y.parent != nil {
						votes[y.parent]++
						if best == nil || votes[y.parent] > votes[best] {
							best = y.parent
						}
					}
				}
				if !take(c, best) {
					var free []*node
					for _, y := range byName[clean(c.n.Title)] {
						if !used[y.n.ID] {
							free = append(free, y)
						}
					}
					if len(free) == 1 {
						take(c, free[0])
					}
				}
			}
			walk(c)
		}
	}
	walk(tl.roots[Bar])
	walk(tl.roots[Other])
	return out
}

func display(path string) string {
	r, rest, _ := strings.Cut(path, "/")
	name := map[string]string{Bar: "Bookmarks Bar", Other: "Other bookmarks", Mobile: "Mobile bookmarks"}[r]
	if rest == "" {
		return name
	}
	parts := strings.Split(rest, "/")
	for i, p := range parts {
		parts[i] = unsegment(p)
	}
	return name + "/" + strings.Join(parts, "/")
}

func item(x *node) Item {
	return Item{ID: x.n.ID, Title: x.n.Title, URL: x.n.URL, Folder: display(x.parent.path), IsFolder: x.n.Kind == store.KindFolder}
}

func moved(from, to *node) Item {
	it := item(to)
	it.Folder, it.To = display(from.parent.path), display(to.parent.path)
	return it
}

func renamed(x *node, was string) Item {
	it := item(x)
	it.Was = was
	return it
}

// diff fills the report with what changes between theirs and the target, by id.
func diff(rep *Report, theirs, target store.Tree) {
	type at struct {
		n      *store.Node
		parent string
		folder string
	}
	index := func(t store.Tree) map[string]at {
		out := map[string]at{}
		var walk func(n *store.Node, path string)
		walk = func(n *store.Node, path string) {
			for _, c := range n.Children {
				out[c.ID] = at{c, n.ID, path}
				walk(c, path+"/"+c.Title)
			}
		}
		walk(t.Root, "")
		return out
	}
	was, now := index(theirs), index(target)
	it := func(a at) Item {
		return Item{ID: a.n.ID, Title: a.n.Title, URL: a.n.URL, Folder: strings.TrimPrefix(a.folder, "/"), IsFolder: a.n.Kind == store.KindFolder}
	}
	for _, id := range sortedKeys(now) {
		a := now[id]
		w, ok := was[id]
		switch {
		case !ok:
			rep.Added = append(rep.Added, it(a))
		default:
			if w.parent != a.parent {
				x := it(w)
				x.To = strings.TrimPrefix(a.folder, "/")
				rep.Moved = append(rep.Moved, x)
			}
			if w.n.Title != a.n.Title || w.n.URL != a.n.URL {
				x := it(a)
				x.Was = w.n.Title
				if w.n.URL != a.n.URL {
					x.WasURL = w.n.URL
				}
				rep.Renamed = append(rep.Renamed, x)
			}
		}
	}
	for _, id := range sortedKeys(was) {
		if _, ok := now[id]; !ok {
			rep.Removed = append(rep.Removed, it(was[id]))
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Fingerprint identifies a tree's ids, titles, addresses and order, so a write can refuse a profile that changed since
// it was planned against.
func Fingerprint(t store.Tree) string {
	type n struct {
		I, T, U string
		C       []n
	}
	var conv func(x *store.Node) n
	conv = func(x *store.Node) n {
		out := n{I: x.ID, T: x.Title, U: x.URL}
		for _, c := range x.Children {
			out.C = append(out.C, conv(c))
		}
		return out
	}
	if t.Root == nil {
		return ""
	}
	b, _ := json.Marshal(conv(t.Root))
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

package edit

import (
	"slices"
	"sort"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

type place struct {
	n       *store.Node
	parent  string
	trashed bool // in the Trash, at its top
	inTrash bool // in the Trash at any depth
}

func places(t store.Tree) map[string]place {
	out := map[string]place{}
	var walk func(p *store.Node, inTrash bool)
	walk = func(p *store.Node, inTrash bool) {
		for _, c := range p.Children {
			out[c.ID] = place{n: c, parent: p.ID, inTrash: inTrash}
			if c.Kind == store.KindFolder {
				walk(c, inTrash)
			}
		}
	}
	if t.Root != nil {
		walk(t.Root, false)
	}
	for _, x := range t.Trash {
		out[x.Node.ID] = place{n: x.Node, parent: x.From, trashed: true, inTrash: true}
		if x.Node.Kind == store.KindFolder {
			walk(x.Node, true)
		}
	}
	return out
}

// Diff describes how next differs from prev in the events Apply would have published, so a whole tree saved at once
// (the web interface saves that way) still tells webhooks exactly what changed. It also checks what is new or changed
// against the same rules Apply uses: URLs that run code are refused, as are overlong titles and notes.
func Diff(prev, next store.Tree) ([]Event, error) {
	if err := next.Validate(); err != nil {
		return nil, refuse("%s", err)
	}
	a, b := places(prev), places(next)
	reordered := reorders(prev, next, a, b)
	var events []Event
	emit := func(n *store.Node, verb string, data map[string]any) {
		data["id"], data["title"] = n.ID, n.Title
		events = append(events, Event{Type: kindOf(n) + "." + verb, Data: data})
	}
	walkOrder := func(t store.Tree, fn func(id string)) {
		var walk func(p *store.Node)
		walk = func(p *store.Node) {
			for _, c := range p.Children {
				fn(c.ID)
				if c.Kind == store.KindFolder {
					walk(c)
				}
			}
		}
		walk(t.Root)
		for _, x := range t.Trash {
			fn(x.Node.ID)
			walk(x.Node)
		}
	}
	var err error
	walkOrder(next, func(id string) {
		if err != nil {
			return
		}
		now := b[id]
		was, existed := a[id]
		changedURL := now.n.Kind == store.KindBookmark && (!existed || was.n.URL != now.n.URL)
		if changedURL {
			if e := CheckURL(now.n.URL); e != nil {
				err = e
				return
			}
		}
		if e := checkText("title", now.n.Title); e != nil {
			err = e
			return
		}
		if e := checkText("note", now.n.Note); e != nil {
			err = e
			return
		}
		if now.n.Kind == store.KindFolder && (now.n.Rating != 0 || len(now.n.Tags) > 0) {
			err = refuse("a folder has no rating or tags")
			return
		}
		if e := CheckRating(now.n.Rating); e != nil {
			err = e
			return
		}
		if len(now.n.Tags) > 0 {
			clean, e := CleanTags(now.n.Tags)
			if e != nil {
				err = e
				return
			}
			now.n.Tags = clean
		}
		switch {
		case !existed:
			if !now.inTrash || now.trashed {
				emit(now.n, "created", map[string]any{"parent": now.parent, "url": now.n.URL})
			}
		case now.trashed && !was.trashed && !was.inTrash:
			emit(now.n, "trashed", map[string]any{"from": was.parent})
		case was.inTrash && !now.inTrash:
			if was.trashed {
				emit(now.n, "restored", map[string]any{"to": now.parent})
			}
		case !now.inTrash && (was.parent != now.parent || reordered[id]):
			emit(now.n, "moved", map[string]any{"from": was.parent, "to": now.parent})
		}
		sameTags := !existed || strings.Join(was.n.Tags, "\x00") == strings.Join(now.n.Tags, "\x00")
		if existed && (was.n.Title != now.n.Title || was.n.URL != now.n.URL || was.n.Note != now.n.Note || was.n.Rating != now.n.Rating || !sameTags) {
			data := map[string]any{}
			if was.n.Title != now.n.Title {
				data["renamed"] = true
			}
			if was.n.Rating != now.n.Rating {
				data["rating"] = now.n.Rating
			}
			if !sameTags {
				data["tags"] = now.n.Tags
			}
			if was.n.URL != now.n.URL {
				data["url"] = now.n.URL
			}
			if was.n.Note != now.n.Note {
				data["note"] = now.n.Note
			}
			emit(now.n, "updated", data)
		}
	})
	if err != nil {
		return nil, err
	}
	deleted := 0
	walkOrder(prev, func(id string) {
		if _, ok := b[id]; !ok && a[id].trashed {
			deleted++
		}
	})
	if deleted > 1 && deleted == len(prev.Trash) && len(next.Trash) == 0 {
		events = append(events, Event{Type: "trash.emptied", Data: map[string]any{"count": deleted}})
	} else {
		walkOrder(prev, func(id string) {
			if _, ok := b[id]; !ok && (a[id].trashed || !a[id].inTrash) {
				emit(a[id].n, "deleted", map[string]any{})
			}
		})
	}
	return events, nil
}

// reorders finds what moved within its own folder: of the children a folder had before and still has, those outside
// the longest run kept in the same order. A neighbour moving away or arriving shifts the others but moves none of them.
func reorders(prev, next store.Tree, a, b map[string]place) map[string]bool {
	moved := map[string]bool{}
	before := map[string][]string{}
	var walk func(p *store.Node, into map[string][]string)
	walk = func(p *store.Node, into map[string][]string) {
		for _, c := range p.Children {
			into[p.ID] = append(into[p.ID], c.ID)
			if c.Kind == store.KindFolder {
				walk(c, into)
			}
		}
	}
	after := map[string][]string{}
	if prev.Root != nil {
		walk(prev.Root, before)
	}
	if next.Root != nil {
		walk(next.Root, after)
	}
	for parent, olds := range before {
		stay := func(list []string, other map[string]place) []string {
			var out []string
			for _, id := range list {
				if p, ok := other[id]; ok && p.parent == parent && !p.inTrash {
					out = append(out, id)
				}
			}
			return out
		}
		x, y := stay(olds, b), stay(after[parent], a)
		keep := lcs(x, y)
		for _, id := range y {
			if !keep[id] {
				moved[id] = true
			}
		}
	}
	return moved
}

// lcs returns the ids in a longest common subsequence of x and y, for lists of distinct ids (a folder's children).
// Each id of y is replaced by its position in x (ids x lacks are dropped), and the longest increasing run of positions
// is found by patience sorting: O(n log n) time and O(n) memory, so a folder of any size is diffed exactly.
func lcs(x, y []string) map[string]bool {
	out := map[string]bool{}
	if slices.Equal(x, y) { // the usual case: nothing reordered
		for _, id := range y {
			out[id] = true
		}
		return out
	}
	at := make(map[string]int, len(x))
	for i, id := range x {
		at[id] = i
	}
	var seq []int    // positions in x, in y's order
	var ids []string // the id at each seq entry
	for _, id := range y {
		if p, ok := at[id]; ok {
			seq, ids = append(seq, p), append(ids, id)
		}
	}
	tails, prev := []int{}, make([]int, len(seq)) // tails[k]: index in seq ending the best run of length k+1
	for i, v := range seq {
		k := sort.Search(len(tails), func(k int) bool { return seq[tails[k]] >= v })
		prev[i] = -1
		if k > 0 {
			prev[i] = tails[k-1]
		}
		if k == len(tails) {
			tails = append(tails, i)
		} else {
			tails[k] = i
		}
	}
	if len(tails) > 0 {
		for i := tails[len(tails)-1]; i >= 0; i = prev[i] {
			out[ids[i]] = true
		}
	}
	return out
}

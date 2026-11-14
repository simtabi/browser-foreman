// Package dupes finds bookmarks in one workspace that lead to the same page, beyond the exact copies a merge catches:
// the same address written differently, links that end up at the same address after redirects, pages that name the
// same canonical address, and pages on one site with the same title. It reads the tree and the link checks; it changes
// nothing (the caller moves the copies it is told to the Trash).
package dupes

import (
	"cmp"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/simtabi/browser-foreman/src/internal/merge"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Reasons two bookmarks are grouped, strongest first.
const (
	SameAddress   = "same-address"   // the same page once tracking parameters, "www." and a trailing slash are set aside
	SameFinal     = "same-final"     // one redirects to the other, or both redirect to the same place
	SameCanonical = "same-canonical" // both pages name the same canonical address
	SimilarTitle  = "similar-title"  // the same page with another query or fragment, and the same title (weakest)
)

// Member is one bookmark of a group. Path is its folder, as the page shows it.
type Member struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Path  string `json:"path"`
	Added int64  `json:"added,omitempty"`
}

// Group is bookmarks that lead to the same page. Reasons says which rules joined them (strongest first); Keep is the
// one to keep by default: the deepest folder (the most specific filing), then the earliest date added.
type Group struct {
	Reasons []string `json:"reasons"`
	Keep    string   `json:"keep"`
	Members []Member `json:"members"`
}

// minTitle is the shortest normalised title the title rule trusts: "Home", "Docs" or "Login" say nothing.
const minTitle = 12

// Find groups the bookmarks of t's live tree (the Trash is left out) that lead to the same page, surest groups first.
func Find(t store.Tree, links map[string]store.LinkResult) []Group {
	var ms []Member
	var depth []int
	var walk func(n *store.Node, path string, d int)
	walk = func(n *store.Node, path string, d int) {
		for _, c := range n.Children {
			switch c.Kind {
			case store.KindFolder:
				walk(c, strings.TrimPrefix(path+"/"+c.Title, "/"), d+1)
			case store.KindBookmark:
				if c.URL != "" {
					ms = append(ms, Member{ID: c.ID, Title: c.Title, URL: c.URL, Path: path, Added: c.Added})
					depth = append(depth, d)
				}
			}
		}
	}
	if t.Root != nil {
		walk(t.Root, "", 0)
	}
	parent := make([]int, len(ms))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	reasons := map[int][]string{} // the rules that joined each group, kept on its root
	join := func(a, b int, rule string) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		parent[rb] = ra
		rs := append(append(reasons[ra], reasons[rb]...), rule)
		delete(reasons, rb)
		reasons[ra] = rs
	}
	byKey := func(rule string, key func(i int) string) {
		first := map[string]int{}
		for i := range ms {
			k := key(i)
			if k == "" {
				continue
			}
			if j, ok := first[k]; ok {
				join(j, i, rule)
			} else {
				first[k] = i
			}
		}
	}
	byKey(SameAddress, func(i int) string { return merge.Key(ms[i].URL) })
	// Where a link ends up: its final address when a check followed a redirect, else its own. A link that redirects to
	// another bookmark's address lands with it.
	byKey(SameFinal, func(i int) string {
		if r, ok := links[ms[i].URL]; ok && r.Final != "" && r.Status == "moved" {
			return merge.Key(r.Final)
		}
		return merge.Key(ms[i].URL)
	})
	byKey(SameCanonical, func(i int) string {
		if r, ok := links[ms[i].URL]; ok && r.Meta != nil && r.Meta.Canonical != "" {
			return merge.Key(r.Meta.Canonical)
		}
		return ""
	})
	// The same page asked with another query or fragment (a search, a tab, a tracking id) says the same thing only when
	// the title agrees too. A title alone is no evidence: sites give many pages one title (demos, search results).
	byKey(SimilarTitle, func(i int) string {
		page := pageOf(ms[i].URL)
		t := normTitle(ms[i].Title)
		if page == "" || len(t) < minTitle {
			return ""
		}
		return page + "\x00" + t
	})
	groups := map[int][]int{}
	for i := range ms {
		r := find(i)
		groups[r] = append(groups[r], i)
	}
	var out []Group
	order := []string{SameAddress, SameFinal, SameCanonical, SimilarTitle}
	for root, idx := range groups {
		if len(idx) < 2 {
			continue
		}
		var rs []string
		for _, rule := range order {
			if slices.Contains(reasons[root], rule) {
				rs = append(rs, rule)
			}
		}
		g := Group{Reasons: rs}
		keep := slices.MinFunc(idx, func(a, b int) int {
			if c := cmp.Compare(depth[b], depth[a]); c != 0 {
				return c
			}
			return cmp.Compare(orMax(ms[a].Added), orMax(ms[b].Added))
		})
		g.Keep = ms[keep].ID
		g.Members = append(g.Members, ms[keep]) // the one to keep first, then the rest in tree order
		for _, i := range idx {
			if i != keep {
				g.Members = append(g.Members, ms[i])
			}
		}
		out = append(out, g)
	}
	// The surest first (by the strongest rule that joined each group), then the largest, then by title.
	slices.SortFunc(out, func(a, b Group) int {
		if c := cmp.Compare(slices.Index(order, a.Reasons[0]), slices.Index(order, b.Reasons[0])); c != 0 {
			return c
		}
		if c := cmp.Compare(len(b.Members), len(a.Members)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Members[0].Title, b.Members[0].Title); c != 0 {
			return c
		}
		return cmp.Compare(a.Keep, b.Keep)
	})
	return out
}

func orMax(t int64) int64 {
	if t == 0 {
		return 1<<63 - 1
	}
	return t
}

// pageOf is a web link's host (lower case, without "www.") and path, without the query, the fragment, a trailing
// slash or a trailing index file; "" for anything else.
func pageOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	path := strings.TrimRight(indexFile.ReplaceAllString(u.EscapedPath(), ""), "/")
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.") + path
}

var indexFile = regexp.MustCompile(`/index\.(html?|php|aspx?)$`)

// normTitle compares titles loosely: lower case, with punctuation and repeated spaces folded, so "A - Site" and
// "A | Site" agree.
func normTitle(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }), " ")
}

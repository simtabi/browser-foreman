package dupes

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func bm(id, title, u string, added int64) *store.Node {
	return &store.Node{ID: id, Kind: store.KindBookmark, Title: title, URL: u, Added: added}
}
func folder(id, title string, c ...*store.Node) *store.Node {
	return &store.Node{ID: id, Kind: store.KindFolder, Title: title, Children: c}
}

// describe renders groups as "reasons: keep, others" for comparing.
func describe(gs []Group) string {
	var out []string
	for _, g := range gs {
		var ids []string
		for _, m := range g.Members {
			ids = append(ids, m.ID)
		}
		out = append(out, fmt.Sprintf("%s: %s", strings.Join(g.Reasons, "+"), strings.Join(ids, ",")))
	}
	return strings.Join(out, " | ")
}

func TestFindGroupsByEveryRule(t *testing.T) {
	tr := store.Tree{Root: folder("root", "",
		folder("bar", "Bookmarks Bar",
			bm("a1", "Go", "https://go.dev/doc/", 300),
			folder("dev", "Dev",
				bm("a2", "Go docs", "https://www.go.dev/doc?utm_source=x", 200), // same address: deeper wins
				bm("f1", "Old blog", "http://blog.example/post", 0),             // redirects to f2's address
				bm("f2", "New blog", "https://blog.example.com/post", 0),
				bm("c1", "Article", "https://news.example/a?id=7", 0), // both name the same canonical address
				bm("c2", "Article (AMP)", "https://amp.news.example/a", 0),
				bm("t1", "Building a REST API in Go - Example Blog", "https://example.org/p/1/index.html?tab=a", 0),
				bm("t2", "Building a REST API in Go | Example Blog", "https://example.org/p/1#comments", 0),
				bm("t3", "Building a REST API in Go - Example Blog", "https://example.org/p/2", 0),   // same title, another page: no
				bm("t4", "Building a REST API in Go - Example Blog", "https://other.example/p/1", 0), // another site: no
				bm("s1", "Home", "https://example.org/?a=1", 0),                                      // short titles: no
				bm("s2", "Home", "https://example.org/?a=2", 0),
				bm("q1", "Search results for go", "https://example.org/search?q=go", 0), // other query, other title: no
				bm("q2", "Search results for rust", "https://example.org/search?q=rust", 0),
			)),
	), Trash: []*store.Trashed{{Node: bm("x", "Go", "https://go.dev/doc", 0)}}} // the Trash is left out
	links := map[string]store.LinkResult{
		"http://blog.example/post":    {Status: "moved", Final: "https://blog.example.com/post/"},
		"https://news.example/a?id=7": {Status: "ok", Meta: &store.PageMeta{Canonical: "https://news.example/a"}},
		"https://amp.news.example/a":  {Status: "ok", Meta: &store.PageMeta{Canonical: "https://news.example/a/"}},
	}
	got := describe(Find(tr, links))
	want := "same-address: a2,a1 | same-final: f1,f2 | same-canonical: c1,c2 | similar-title: t1,t2"
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
}

func TestKeepPrefersTheDeepestThenTheEarliest(t *testing.T) {
	tr := store.Tree{Root: folder("root", "", folder("bar", "Bar",
		bm("late", "X", "https://x.example/", 900),
		bm("early", "X", "https://x.example", 100),
		bm("undated", "X", "https://x.example/?utm_medium=a", 0)))}
	if g := Find(tr, nil); len(g) != 1 || g[0].Keep != "early" || g[0].Members[0].ID != "early" || g[0].Members[0].Path != "Bar" {
		t.Fatalf("%+v", g)
	}
	// Filed deeper beats added earlier.
	tr = store.Tree{Root: folder("root", "", folder("bar", "Bar",
		bm("shallow", "X", "https://x.example/", 100),
		folder("f", "Filed", bm("deep", "X", "https://x.example", 900))))}
	if g := Find(tr, nil); len(g) != 1 || g[0].Keep != "deep" {
		t.Fatalf("%+v", g)
	}
}

func TestADeadOrUnmovedLinkIsNotJoinedByItsFinalAddress(t *testing.T) {
	tr := store.Tree{Root: folder("root", "", folder("bar", "Bar",
		bm("a", "A", "https://a.example/old", 0), bm("b", "B", "https://a.example/", 0)))}
	// A dead page that "ended" at the home page is not the home page.
	links := map[string]store.LinkResult{"https://a.example/old": {Status: "unverified", Final: "https://a.example/"}}
	if g := Find(tr, links); len(g) != 0 {
		t.Fatalf("%s", describe(g))
	}
}

func TestNormTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Building a REST API in Go - Example Blog": "building a rest api in go example blog",
		"Building a REST API in Go | Example Blog": "building a rest api in go example blog",
		"Tabler Icons: over 1250 vector icons":     "tabler icons over 1250 vector icons",
		"  Café — Menu ":                           "café menu",
	} {
		if got := normTitle(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if pageOf("https://WWW.Example.org/a/index.php?x=1#y") != "example.org/a" || pageOf("ftp://x/") != "" {
		t.Fatal(pageOf("https://WWW.Example.org/a/index.php?x=1#y"))
	}
}

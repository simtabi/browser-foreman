package exporter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func sample() store.Tree {
	return store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "a", Kind: store.KindFolder, Title: "Bookmarks Bar", Added: 1_700_000_000, Children: []*store.Node{
			{ID: "b", Kind: store.KindBookmark, Title: `Fish & "chips" <b>`, URL: "https://example.com/?a=1&b=2", Added: 1_700_000_100, Note: "tasty"},
			{ID: "c", Kind: store.KindFolder, Title: "Dev", Children: []*store.Node{
				{ID: "d", Kind: store.KindBookmark, Title: "Go", URL: "https://go.dev/"}}},
		}},
		{ID: "e", Kind: store.KindFolder, Title: "Other bookmarks", Children: []*store.Node{
			{ID: "f", Kind: store.KindBookmark, Title: "Ünïcode ✓", URL: "https://例え.jp/"}}},
	}}}
}

// shape prints titles, URLs, dates and notes, ignoring ids, which each reader assigns afresh.
func shape(n *store.Node, notes bool) string {
	var b strings.Builder
	for _, c := range n.Children {
		b.WriteString("[" + c.Title + "|" + c.URL)
		if notes {
			b.WriteString("|" + c.Note)
		}
		if c.Added > 0 {
			b.WriteString("|t")
		}
		b.WriteString(shape(c, notes) + "]")
	}
	return b.String()
}

func TestNetscapeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := Netscape(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	back, format, err := importer.Read(&buf)
	if err != nil || format != importer.Netscape {
		t.Fatal(format, err)
	}
	if got, want := shape(back.Root, true), shape(sample().Root, true); got != want {
		t.Fatalf("round trip changed the tree\n got %s\nwant %s", got, want)
	}
	if back.Root.Children[0].Children[0].Added != 1_700_000_100 {
		t.Fatal("date lost")
	}
}

func TestChromeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := Chrome(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	back, format, err := importer.Read(&buf)
	if err != nil || format != importer.Chrome {
		t.Fatal(format, err)
	}
	if got, want := shape(back.Root, false), shape(sample().Root, false); got != want {
		t.Fatalf("round trip changed the tree\n got %s\nwant %s", got, want)
	}
	if back.Root.Children[0].Children[0].Added != 1_700_000_100 {
		t.Fatalf("date: %d", back.Root.Children[0].Children[0].Added)
	}
}

func TestChromeFoldsStrayTopFoldersIntoOther(t *testing.T) {
	tr := sample()
	tr.Root.Children = append(tr.Root.Children, &store.Node{ID: "z", Kind: store.KindFolder, Title: "Imported from Safari"})
	var buf bytes.Buffer
	_ = Chrome(&buf, tr)
	back, _, err := importer.Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	other := back.Root.Children[1]
	if other.Title != "Other bookmarks" || other.Children[len(other.Children)-1].Title != "Imported from Safari" {
		t.Fatal(shape(back.Root, false))
	}
}

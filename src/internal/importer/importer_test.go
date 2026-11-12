package importer

import (
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

const chromeFile = `{
 "checksum": "x", "version": 1,
 "roots": {
  "bookmark_bar": {"type": "folder", "name": "Bookmarks bar", "date_added": "13300000000000000", "children": [
    {"type": "url", "name": "Go", "url": "https://go.dev/", "date_added": "13300000000000000"},
    {"type": "folder", "name": "Dev", "children": [
      {"type": "url", "name": "  Laravel   docs ", "url": "https://laravel.com/docs"}
    ]}
  ]},
  "other": {"type": "folder", "name": "Other bookmarks", "children": [
    {"type": "url", "name": "Example", "url": "https://example.com/"}
  ]},
  "synced": {"type": "folder", "name": "Mobile bookmarks", "children": []}
 }
}`

const netscapeFile = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">
<TITLE>Bookmarks</TITLE>
<H1>Bookmarks</H1>
<DL><p>
    <DT><H3 ADD_DATE="1700000000" PERSONAL_TOOLBAR_FOLDER="true">Bookmarks bar</H3>
    <DL><p>
        <DT><A HREF="https://go.dev/" ADD_DATE="1700000001">Go &amp; friends</A>
        <DD>The Go website
        <DT><H3>Dev</H3>
        <DL><p>
            <DT><A HREF="https://laravel.com/docs" ADD_DATE="1700000002000">Laravel</A>
        </DL><p>
    </DL><p>
    <DT><A HREF="https://example.com/">Loose at the top</A>
</DL><p>
`

func shape(n *store.Node, depth int, b *strings.Builder) {
	for _, c := range n.Children {
		b.WriteString(strings.Repeat("  ", depth))
		if c.Kind == store.KindFolder {
			b.WriteString("[" + c.Title + "]\n")
			shape(c, depth+1, b)
		} else {
			b.WriteString(c.Title + " " + c.URL + "\n")
		}
	}
}

func TestChromeFile(t *testing.T) {
	tr, f, err := Read(strings.NewReader(chromeFile))
	if err != nil || f != Chrome {
		t.Fatal(f, err)
	}
	var b strings.Builder
	shape(tr.Root, 0, &b)
	want := "[Bookmarks Bar]\n  Go https://go.dev/\n  [Dev]\n    Laravel docs https://laravel.com/docs\n[Other bookmarks]\n  Example https://example.com/\n"
	if b.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
	}
	if go_ := tr.Root.Children[0].Children[0]; go_.Added != 1655526400 {
		t.Fatalf("Chrome's 1601-based microseconds: got %d", go_.Added)
	}
	if bm, fo := tr.Count(); bm != 3 || fo != 3 {
		t.Fatalf("count %d %d", bm, fo)
	}
}

func TestNetscapeFile(t *testing.T) {
	tr, f, err := Read(strings.NewReader(netscapeFile))
	if err != nil || f != Netscape {
		t.Fatal(f, err)
	}
	var b strings.Builder
	shape(tr.Root, 0, &b)
	want := "[Bookmarks bar]\n  Go & friends https://go.dev/\n  [Dev]\n    Laravel https://laravel.com/docs\nLoose at the top https://example.com/\n"
	if b.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
	}
	goNode := tr.Root.Children[0].Children[0]
	if goNode.Note != "The Go website" || goNode.Added != 1700000001 {
		t.Fatalf("note %q added %d", goNode.Note, goNode.Added)
	}
	if lv := tr.Root.Children[0].Children[1].Children[0]; lv.Added != 1700000002 {
		t.Fatalf("milliseconds become seconds: %d", lv.Added)
	}
}

func TestSameFileSameTree(t *testing.T) {
	a, _, _ := Read(strings.NewReader(netscapeFile))
	b, _, _ := Read(strings.NewReader(netscapeFile))
	ha, _, _ := store.Hash(a)
	hb, _, _ := store.Hash(b)
	if ha != hb {
		t.Fatal("reading the same file twice gave different trees")
	}
}

func TestNotABookmarksFile(t *testing.T) {
	for _, s := range []string{"hello", "", "<html><body>no list</body></html>", `{"roots": {}}`} {
		if _, _, err := Read(strings.NewReader(s)); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
}

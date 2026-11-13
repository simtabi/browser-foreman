package merge

import (
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestUpdateAddsNewTakesTitlesAndKeepsWhatIsGone(t *testing.T) {
	before := tree(folder("bar", "Bookmarks Bar",
		bm("1", "Go", "https://go.dev/", 0), bm("2", "Old title", "https://a.example/", 0),
		bm("3", "Renamed upstream too", "https://b.example/", 0), bm("4", "Leaving", "https://gone.example/", 0),
		bm("7", "Spaces ", "https://spaces.example/", 0)))
	ws, err := Merge(store.Tree{}, []Source{{"Bookmarks", before}}, false, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Here: b.example renamed by hand; gone.example kept.
	for _, c := range ws.Tree.Root.Children[0].Children {
		if c.URL == "https://b.example/" {
			c.Title = "My own name"
		}
	}
	now := tree(folder("bar", "Bookmarks Bar",
		bm("1", "Go", "https://www.go.dev", 0), bm("2", "New title", "https://a.example/", 0),
		bm("3", "Upstream name", "https://b.example/", 0), bm("7", "Spaces", "https://spaces.example/", 0),
		folder("f", "Fresh", bm("5", "Fresh", "https://fresh.example/", 0), bm("6", "Fresh again", "https://fresh.example", 0))))
	ups := []Upstream{{Name: "Bookmarks", Before: before, Now: now}}
	p := PlanUpdate(ws.Tree, ups)
	if len(p.New) != 1 || p.New[0].URL != "https://fresh.example/" || p.New[0].Folder != "Bookmarks Bar/Fresh" {
		t.Fatalf("new %+v", p.New)
	}
	if len(p.Retitle) != 1 || p.Retitle[0].NewTitle != "New title" {
		t.Fatalf("retitle %+v (a title renamed here must not be taken)", p.Retitle)
	}
	if len(p.Gone) != 1 || p.Gone[0].URL != "https://gone.example/" {
		t.Fatalf("gone %+v", p.Gone)
	}
	res, err := ApplyUpdate(ws.Tree, ups, p, 2)
	if err != nil {
		t.Fatal(err)
	}
	bar := find(res.Tree.Root, "Bookmarks Bar")
	titles := map[string]string{}
	for _, c := range bar.Children {
		titles[c.URL] = c.Title
	}
	if titles["https://a.example/"] != "New title" || titles["https://b.example/"] != "My own name" || titles["https://gone.example/"] != "Leaving" {
		t.Fatalf("titles %v", titles)
	}
	if f := find(res.Tree.Root, "Bookmarks Bar", "Fresh"); f == nil || len(f.Children) != 1 || res.Added != 1 || len(res.Dups) != 1 {
		t.Fatalf("fresh folder %+v added %d dups %d", f, res.Added, len(res.Dups))
	}
	if again := PlanUpdate(res.Tree, []Upstream{{Name: "Bookmarks", Before: now, Now: now}}); len(again.New)+len(again.Retitle)+len(again.Gone) != 0 {
		t.Fatalf("a second update found %+v", again)
	}
}

func TestUpdateIgnoresWhatIsInTheTrash(t *testing.T) {
	ws := store.Tree{Root: folder("root", ""), Trash: []*store.Trashed{{Node: bm("x", "Thrown away", "https://away.example/", 0), From: "root"}}}
	p := PlanUpdate(ws, []Upstream{{Name: "s", Before: tree(), Now: tree(bm("1", "Away", "https://away.example", 0))}})
	if len(p.New) != 0 {
		t.Fatalf("a bookmark in the Trash came back as new: %+v", p.New)
	}
}

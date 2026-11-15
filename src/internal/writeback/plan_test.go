package writeback

import (
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func f(id, title string, kids ...*store.Node) *store.Node {
	return &store.Node{ID: id, Kind: store.KindFolder, Title: title, Children: kids}
}

func b(id, title, url string) *store.Node {
	return &store.Node{ID: id, Kind: store.KindBookmark, Title: title, URL: url}
}

func tree(kids ...*store.Node) store.Tree { return store.Tree{Root: f("root", "", kids...)} }

// The export of 2026-11-10 as the importer reads it: loose items at the top are Other bookmarks.
func base() store.Tree {
	return tree(
		f("n1", "Bookmarks Bar"),
		b("n2", "A", "https://a.example/"), b("n3", "B", "https://b.example/"), b("n4", "C", "https://c.example/"),
		b("n5", "A copy", "https://www.a.example"), b("n6", "E", "https://e.example/"),
		f("n7", "Dev", b("n8", "D", "https://d.example/")),
	)
}

// The cleanup: everything under the bar, A renamed into a new folder, C and the copy of A removed.
func ours() store.Tree {
	return tree(f("w1", "Bookmarks Bar",
		f("w2", "Dev", b("w3", "D", "https://d.example/")),
		f("w4", "Reading", &store.Node{ID: "w5", Kind: store.KindBookmark, Title: "Alpha", URL: "https://a.example/", Note: "kept here"}),
		b("w6", "B", "https://b.example/"),
		b("w7", "E", "https://e.example/"),
	))
}

// Chrome now: B renamed, E deleted, N added, the Dev folder's title has a trailing space, one phone bookmark.
func theirs() store.Tree {
	return tree(
		f("1", "Bookmarks bar"),
		f("2", "Other bookmarks",
			b("11", "A", "https://a.example/"), b("12", "Bee", "https://b.example/"), b("13", "C", "https://c.example/"),
			b("14", "A copy", "https://www.a.example"),
			f("15", "Dev ", b("16", "D", "https://d.example/")),
			b("17", "N", "https://n.example/")),
		f("3", "Mobile bookmarks", b("31", "M", "https://m.example/")),
	)
}

type flat struct{ id, title, url, folder string }

func flatten(t store.Tree) map[string]flat {
	out := map[string]flat{}
	var walk func(n *store.Node, path string)
	walk = func(n *store.Node, path string) {
		for _, c := range n.Children {
			key := c.URL
			if c.Kind == store.KindFolder {
				key = "folder:" + strings.TrimSpace(c.Title)
			}
			out[key] = flat{c.ID, c.Title, c.URL, path}
			walk(c, strings.TrimPrefix(path+"/"+c.ID, "/"))
		}
	}
	walk(t.Root, "")
	return out
}

func titles(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

func TestMergeAppliesTheCleanupAndKeepsWhatChromeChanged(t *testing.T) {
	target, rep, err := Plan(base(), ours(), theirs(), Merge)
	if err != nil {
		t.Fatal(err)
	}
	got := flatten(target)
	want := map[string]flat{
		"folder:Bookmarks bar":    {"1", "Bookmarks bar", "", ""},
		"folder:Other bookmarks":  {"2", "Other bookmarks", "", ""},
		"folder:Mobile bookmarks": {"3", "Mobile bookmarks", "", ""},
		"folder:Dev":              {"15", "Dev ", "", "1"}, // Chrome's folder and its own title
		"https://d.example/":      {"16", "D", "https://d.example/", "1/15"},
		"folder:Reading":          {"new-1", "Reading", "", "1"}, // made by the cleanup
		"https://a.example/":      {"11", "Alpha", "https://a.example/", "1/new-1"},
		"https://b.example/":      {"12", "Bee", "https://b.example/", "1"}, // renamed in Chrome, not by the cleanup
		"https://n.example/":      {"17", "N", "https://n.example/", "2"},   // added in Chrome after the export
		"https://m.example/":      {"31", "M", "https://m.example/", "3"},
	}
	if len(got) != len(want) {
		t.Errorf("target holds %d items, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %+v, want %+v", k, got[k], w)
		}
	}
	if s := titles(rep.AddedSince); len(s) != 1 || s[0] != "N" {
		t.Errorf("added since %v", s)
	}
	if s := titles(rep.RemovedSince); len(s) != 1 || s[0] != "E" {
		t.Errorf("removed since %v (E was deleted in Chrome: never brought back)", s)
	}
	if s := titles(rep.RenamedSince); len(s) != 1 || s[0] != "Bee" {
		t.Errorf("renamed since %v", s)
	}
	if s := titles(rep.Removed); len(s) != 2 || s[0] != "C" || s[1] != "A copy" {
		t.Errorf("removed %v, want C and the copy of A", s)
	}
	if s := titles(rep.Added); len(s) != 1 || s[0] != "Reading" {
		t.Errorf("added %v", s)
	}
	if len(rep.Renamed) != 1 || rep.Renamed[0].Title != "Alpha" || rep.Renamed[0].Was != "A" {
		t.Errorf("renamed %+v", rep.Renamed)
	}
	if rep.NotWritten != 1 || rep.DropNew != 0 || len(rep.Conflicts) != 0 {
		t.Errorf("not written %d, drop new %d, conflicts %v", rep.NotWritten, rep.DropNew, rep.Conflicts)
	}
}

func TestReplaceDropsWhatChromeGainedOnlyByCount(t *testing.T) {
	target, rep, err := Plan(base(), ours(), theirs(), Replace)
	if err != nil {
		t.Fatal(err)
	}
	got := flatten(target)
	if _, ok := got["https://n.example/"]; ok || rep.DropNew != 1 {
		t.Fatalf("replace kept N or did not count it: drop new %d", rep.DropNew)
	}
	if got["https://b.example/"].title != "B" {
		t.Errorf("replace keeps the cleanup's title: %+v", got["https://b.example/"])
	}
	if _, ok := got["https://e.example/"]; ok {
		t.Error("E, deleted in Chrome after the export, was brought back")
	}
	if got["https://m.example/"].id != "31" {
		t.Error("Mobile bookmarks changed")
	}
}

func TestApplyingTheSamePlanTwiceChangesNothing(t *testing.T) {
	for _, mode := range []Mode{Merge, Replace} {
		first, _, err := Plan(base(), ours(), theirs(), mode)
		if err != nil {
			t.Fatal(err)
		}
		_, again, err := Plan(base(), ours(), first, mode)
		if err != nil {
			t.Fatal(err)
		}
		if !again.Empty() || again.DropNew != 0 {
			t.Errorf("%s: a second plan against the written result still changes %+v", mode, again)
		}
	}
}

func TestBothSidesMovingIsAConflictTheCleanupWins(t *testing.T) {
	th := theirs()
	// In Chrome, D moved out of Dev to the bar; the cleanup kept it in Dev. Then the cleanup moves it too.
	th.Root.Children[1].Children[4].Children = nil
	th.Root.Children[0].Children = []*store.Node{b("16", "D", "https://d.example/")}
	target, rep, err := Plan(base(), ours(), th, Merge)
	if err != nil {
		t.Fatal(err)
	}
	if got := flatten(target)["https://d.example/"]; got.folder != "1" || len(rep.MovedSince) != 1 {
		t.Fatalf("Chrome's move of D was not kept when the cleanup left D alone: %+v %v", got, rep.MovedSince)
	}
	o := ours()
	o.Root.Children[0].Children[1].Children = append(o.Root.Children[0].Children[1].Children, b("w8", "D", "https://d.example/"))
	o.Root.Children[0].Children[0].Children = nil
	target, rep, _ = Plan(base(), o, th, Merge)
	if got := flatten(target)["https://d.example/"]; got.folder != "1/new-1" || len(rep.Conflicts) != 1 {
		t.Fatalf("both moved D: %+v, conflicts %v", got, rep.Conflicts)
	}
}

func TestFingerprintFollowsTheTree(t *testing.T) {
	a, c := theirs(), theirs()
	if Fingerprint(a) != Fingerprint(c) {
		t.Fatal("the same tree has two fingerprints")
	}
	c.Root.Children[1].Children[0].Title = "A!"
	if Fingerprint(a) == Fingerprint(c) {
		t.Fatal("a renamed bookmark kept the fingerprint")
	}
	if _, rep, _ := Plan(base(), ours(), a, Merge); rep.Fingerprint != Fingerprint(a) {
		t.Fatal("the report does not carry theirs' fingerprint")
	}
}

func TestPlanRefusesAnUnknownMode(t *testing.T) {
	if _, _, err := Plan(base(), ours(), theirs(), "sync"); err == nil {
		t.Fatal("an unknown mode was accepted")
	}
}

func TestAChangedAddressKeepsTheBookmark(t *testing.T) {
	o := ours()
	o.Root.Children[0].Children[0].Children[0].URL = "https://d.example/moved-here" // the cleanup followed a redirect
	target, rep, err := Plan(base(), o, theirs(), Merge)
	if err != nil {
		t.Fatal(err)
	}
	if got := flatten(target)["https://d.example/moved-here"]; got.id != "16" {
		t.Fatalf("the bookmark was not kept under its id: %+v", got)
	}
	var found bool
	for _, r := range rep.Renamed {
		found = found || r.WasURL == "https://d.example/"
	}
	if !found || len(rep.Added) != 1 || len(rep.Removed) != 2 {
		t.Fatalf("renamed %+v, added %v, removed %v", rep.Renamed, titles(rep.Added), titles(rep.Removed))
	}
}

func TestOnlyWhitespaceDifferingKeepsChromesTitle(t *testing.T) {
	th := theirs()
	th.Root.Children[1].Children[4].Children[0].Title = " D  " // Chrome keeps whatever spacing it was given
	target, rep, err := Plan(base(), ours(), th, Merge)
	if err != nil {
		t.Fatal(err)
	}
	if got := flatten(target)["https://d.example/"]; got.title != " D  " {
		t.Fatalf("title %q, want Chrome's own", got.title)
	}
	for _, r := range rep.Renamed {
		if r.URL == "https://d.example/" {
			t.Fatalf("a whitespace-only difference is a rename: %+v", r)
		}
	}
}

func TestAMovedFolderIsFoundByItsBookmarksWhenItsNameIsNotUnique(t *testing.T) {
	th := theirs()
	other := th.Root.Children[1]
	other.Children = append(other.Children, f("18", "Old", f("19", "Dev")))
	target, _, err := Plan(base(), ours(), th, Merge)
	if err != nil {
		t.Fatal(err)
	}
	if got := flatten(target)["folder:Dev"]; got.id != "15" {
		t.Fatalf("Dev is %+v, want Chrome's folder 15, which holds its bookmark", got)
	}
}

package writeback

import (
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// folderTitles lists the titles of the folders directly in n, in order.
func folderTitles(n *store.Node) []string {
	var out []string
	for _, c := range n.Children {
		if c.Kind == store.KindFolder {
			out = append(out, c.Title)
		}
	}
	return out
}

func findFolder(n *store.Node, title string) *store.Node {
	for _, c := range n.Children {
		if c.Kind == store.KindFolder && c.Title == title {
			return c
		}
	}
	return nil
}

// A "/" in a folder's title is part of that title. Chrome's own new folder "CI/CD", holding a bookmark added there after
// the export, is made in the target as one folder, not as a folder "CI" holding "CI/CD".
func TestASlashInAFolderTitleIsOneFolder(t *testing.T) {
	was := tree(f("n1", "Bookmarks Bar", b("n2", "A", "https://a.example/")))
	ours := tree(f("w1", "Bookmarks Bar", b("w2", "A", "https://a.example/")))
	theirs := tree(f("1", "Bookmarks bar", b("11", "A", "https://a.example/"), f("12", "CI/CD", b("13", "New", "https://new.example/"))),
		f("2", "Other bookmarks"), f("3", "Mobile bookmarks"))
	target, _, err := Plan(was, ours, theirs, Merge)
	if err != nil {
		t.Fatal(err)
	}
	bar := target.Root.Children[0]
	if got := folderTitles(bar); len(got) != 1 || got[0] != "CI/CD" {
		t.Fatalf("folders in the bar: %q", got)
	}
	if ci := findFolder(bar, "CI/CD"); ci == nil || len(ci.Children) != 1 || ci.Children[0].URL != "https://new.example/" {
		t.Fatalf("CI/CD holds %+v", ci)
	}
}

// Two folders with one name side by side stay two: a bookmark added in Chrome to the second goes to the second.
func TestTwoFoldersWithOneNameStayApart(t *testing.T) {
	work := func(id1, id2, a, bid string, extra ...*store.Node) []*store.Node {
		second := append([]*store.Node{b(bid, "B", "https://b.example/")}, extra...)
		return []*store.Node{f(id1, "Work", b(a, "A", "https://a.example/")), f(id2, "Work", second...)}
	}
	was := tree(f("n1", "Bookmarks Bar", work("n2", "n4", "n3", "n5")...))
	ours := tree(f("w1", "Bookmarks Bar", work("w2", "w4", "w3", "w5")...))
	theirs := tree(f("1", "Bookmarks bar", work("11", "13", "12", "14", b("15", "New", "https://new.example/"))...),
		f("2", "Other bookmarks"), f("3", "Mobile bookmarks"))
	target, _, err := Plan(was, ours, theirs, Merge)
	if err != nil {
		t.Fatal(err)
	}
	bar := target.Root.Children[0]
	if len(bar.Children) != 2 {
		t.Fatalf("bar holds %d items", len(bar.Children))
	}
	first, second := bar.Children[0], bar.Children[1]
	if len(first.Children) != 1 || len(second.Children) != 2 || second.Children[1].URL != "https://new.example/" {
		t.Fatalf("first holds %d, second %d: the new bookmark went to the wrong Work", len(first.Children), len(second.Children))
	}
}

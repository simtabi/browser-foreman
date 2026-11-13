package importer

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"howett.net/plist"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// writePlaces makes a small places.sqlite as Firefox lays it out.
func writePlaces(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		`CREATE TABLE moz_places (id INTEGER PRIMARY KEY, url TEXT)`,
		`CREATE TABLE moz_bookmarks (id INTEGER PRIMARY KEY, type INTEGER, fk INTEGER, parent INTEGER, position INTEGER, title TEXT, dateAdded INTEGER, guid TEXT)`,
		`INSERT INTO moz_places VALUES (1, 'https://go.dev/'), (2, 'place:sort=8'), (3, 'https://news.example/')`,
		`INSERT INTO moz_bookmarks VALUES (1, 2, NULL, 0, 0, '', 0, 'root________'),
			(2, 2, NULL, 1, 0, 'toolbar', 0, 'toolbar_____'), (3, 2, NULL, 1, 1, 'menu', 0, 'menu________'),
			(4, 2, NULL, 1, 2, 'unfiled', 0, 'unfiled_____'), (5, 2, NULL, 1, 3, 'mobile', 0, 'mobile______'),
			(10, 1, 1, 2, 0, 'Go', 1700000000000000, 'a'), (11, 2, NULL, 2, 1, 'Dev', 0, 'b'),
			(12, 1, 3, 11, 0, 'News', 0, 'c'), (13, 3, NULL, 2, 2, '', 0, 'd'), (14, 1, 2, 3, 0, 'Recent', 0, 'e')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFirefoxPlaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "places.sqlite")
	writePlaces(t, path)
	tr, f, err := ReadFile(path)
	if err != nil || f != Firefox {
		t.Fatalf("%v %v", f, err)
	}
	if s := titles(tr.Root); s != "Bookmarks Bar(Go Dev(News)) Bookmarks Menu() Other bookmarks()" {
		t.Fatalf("tree %q (no separators, no place: queries, an empty mobile root left out)", s)
	}
	if tr.Root.Children[0].Children[0].Added != 1700000000 {
		t.Fatal("dateAdded is microseconds")
	}
	raw, _ := os.ReadFile(path)
	if tr2, f, err := Read(strings.NewReader(string(raw))); err != nil || f != Firefox || titles(tr2.Root) != titles(tr.Root) {
		t.Fatalf("as bytes: %v %v", f, err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(raw) {
		t.Fatal("the live database was changed")
	}
}

func TestSafariPlist(t *testing.T) {
	added := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	doc := map[string]any{"WebBookmarkType": "WebBookmarkTypeList", "Title": "", "Children": []any{
		map[string]any{"WebBookmarkType": "WebBookmarkTypeProxy", "Title": "History"},
		map[string]any{"WebBookmarkType": "WebBookmarkTypeList", "Title": "History", "Children": []any{ // as Python guarded against
			map[string]any{"WebBookmarkType": "WebBookmarkTypeLeaf", "URLString": "https://visited.example/"}}},
		map[string]any{"WebBookmarkType": "WebBookmarkTypeList", "Title": "BookmarksBar", "Children": []any{
			map[string]any{"WebBookmarkType": "WebBookmarkTypeLeaf", "URLString": "https://go.dev/", "URIDictionary": map[string]any{"title": "Go"}},
			map[string]any{"WebBookmarkType": "WebBookmarkTypeList", "Title": "Dev", "Children": []any{}},
		}},
		map[string]any{"WebBookmarkType": "WebBookmarkTypeList", "Title": "com.apple.ReadingList", "Children": []any{
			map[string]any{"WebBookmarkType": "WebBookmarkTypeLeaf", "URLString": "https://read.example/", "URIDictionary": map[string]any{"title": "Later"},
				"ReadingList": map[string]any{"DateAdded": added, "PreviewText": "A long read"}},
		}},
	}}
	b, err := plist.Marshal(doc, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Bookmarks.plist")
	_ = os.WriteFile(path, b, 0o600)
	tr, f, err := ReadFile(path)
	if err != nil || f != Safari {
		t.Fatalf("%v %v", f, err)
	}
	if s := titles(tr.Root); s != "Bookmarks Bar(Go Dev()) Reading List(Later)" {
		t.Fatalf("tree %q", s)
	}
	later := tr.Root.Children[1].Children[0]
	if later.Added != added.Unix() || later.Description != "A long read" {
		t.Fatalf("reading list item %+v", later)
	}
}

func TestReadFileOpensOnlyBookmarksFiles(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "id_rsa")
	_ = os.WriteFile(secret, []byte("<DL><DT><A HREF=\"https://x.example/\">x</A>"), 0o600)
	if _, _, err := ReadFile(secret); err != ErrNotBookmarksName {
		t.Fatalf("a file that is not named like bookmarks was read: %v", err)
	}
	link := filepath.Join(dir, "Bookmarks")
	_ = os.Symlink(secret, link)
	if _, _, err := ReadFile(link); err != ErrNotBookmarksName {
		t.Fatalf("a link named Bookmarks reached another file: %v", err)
	}
	if _, _, err := ReadFile(filepath.Join(dir, "missing.html")); err == nil || !strings.Contains(err.Error(), "no file at") {
		t.Fatalf("%v", err)
	}
}

func TestFindProfiles(t *testing.T) {
	dir := t.TempDir()
	ud := filepath.Join(dir, "Chrome")
	for _, p := range []string{"Default", "Profile 1"} {
		_ = os.MkdirAll(filepath.Join(ud, p), 0o700)
		_ = os.WriteFile(filepath.Join(ud, p, "Bookmarks"), []byte(`{"roots":{}}`), 0o600)
	}
	_ = os.WriteFile(filepath.Join(ud, "Local State"), []byte(`{"profile":{"info_cache":{"Default":{"name":"Personal"},"Profile 1":{"name":"Work"}}}}`), 0o600)
	_ = os.Chmod(filepath.Join(ud, "Profile 1", "Bookmarks"), 0)
	locked := filepath.Join(dir, "Locked")
	_ = os.MkdirAll(locked, 0o700)
	_ = os.Chmod(locked, 0)
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	got := findProfiles([]place{chromium("Chrome", ud), {"Safari", filepath.Join(locked, "Bookmarks.plist"), ""}, {"Nothing", filepath.Join(dir, "none", "*", "x"), ""}})
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0].Name != "Personal" || !got[0].Readable || got[1].Name != "Work" || got[1].Readable || got[1].Why == "" {
		t.Fatalf("chrome %+v", got[:2])
	}
	if got[2].Browser != "Safari" || got[2].Readable || got[2].Why == "" {
		t.Fatalf("a folder the system keeps closed must be reported: %+v", got[2])
	}
}

func titles(n *store.Node) string {
	var b []string
	for _, c := range n.Children {
		s := c.Title
		if c.Kind == store.KindFolder {
			s += "(" + titles(c) + ")"
		}
		b = append(b, s)
	}
	return strings.Join(b, " ")
}

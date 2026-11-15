package chromefile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// pythonChecksum is the fixture's checksum as the Python tool's chrome_checksum computed it: an independent
// implementation of the same algorithm.
const pythonChecksum = "b99cf568490cf01317b4a3bba57623cd"

func fixture(t *testing.T) (*Doc, store.Tree, []byte) {
	t.Helper()
	b, err := os.ReadFile("testdata/Bookmarks")
	if err != nil {
		t.Fatal(err)
	}
	d, tr, err := Read(b)
	if err != nil {
		t.Fatal(err)
	}
	return d, tr, b
}

func guids() func() (string, error) {
	n := 0
	return func() (string, error) { n++; return fmt.Sprintf("10000000-0000-4000-8000-%012d", n), nil }
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func find(t *testing.T, doc map[string]any, id string) map[string]any {
	t.Helper()
	var walk func(v any) map[string]any
	walk = func(v any) map[string]any {
		m := v.(map[string]any)
		if m["id"] == id {
			return m
		}
		kids, _ := m["children"].([]any)
		for _, k := range kids {
			if r := walk(k); r != nil {
				return r
			}
		}
		return nil
	}
	for _, k := range rootKeys {
		if r := walk(doc["roots"].(map[string]any)[k]); r != nil {
			return r
		}
	}
	t.Fatalf("no node %s", id)
	return nil
}

func TestReadKeepsTheProfilesIdsAndTitles(t *testing.T) {
	d, tr, _ := fixture(t)
	if len(tr.Root.Children) != 3 || tr.Root.Children[2].ID != "3" {
		t.Fatalf("roots %+v", tr.Root.Children)
	}
	dev := tr.Root.Children[0].Children[1]
	if dev.ID != "6" || dev.Title != "Dev " || dev.Children[0].Title != "Café ☕ 🚀" {
		t.Fatalf("Dev %+v", dev)
	}
	if !d.SyncOn() {
		t.Fatal("sync_metadata present, but SyncOn says no")
	}
}

func TestChecksumMatchesThePythonImplementation(t *testing.T) {
	_, _, b := fixture(t)
	doc := decode(t, b)
	sum, err := Checksum(doc["roots"].(map[string]any))
	if err != nil {
		t.Fatal(err)
	}
	if sum != pythonChecksum || doc["checksum"] != pythonChecksum {
		t.Fatalf("checksum %s, want %s", sum, pythonChecksum)
	}
}

func TestEncodingTheTreeAsReadChangesNothingChromeChecks(t *testing.T) {
	d, tr, _ := fixture(t)
	out, err := d.Encode(tr, time.Now(), guids())
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, out)
	if doc["checksum"] != pythonChecksum || doc["sync_metadata"] != "c3luYw==" || doc["version"] != float64(1) {
		t.Fatalf("checksum %v, sync_metadata %v, version %v", doc["checksum"], doc["sync_metadata"], doc["version"])
	}
	if m := find(t, doc, "8"); m["guid"] != "00000000-0000-4000-8000-000000000008" || m["meta_info"].(map[string]any)["power_bookmark_meta"] != "abc" {
		t.Fatalf("node 8 lost its fields: %v", m)
	}
	if !strings.Contains(string(out), "Caf\u00e9 \u2615 \U0001F680") || !strings.Contains(string(out), "a=1&b=2") || strings.Contains(string(out), "\\u0026") {
		t.Fatal("titles or addresses were escaped")
	}
}

func TestEncodeMovesRenamesAndAddsWithNewIds(t *testing.T) {
	d, tr, _ := fixture(t)
	bar, other := tr.Root.Children[0], tr.Root.Children[1]
	news := other.Children[0]
	news.Title = "Daily news"
	bar.Title = "Bookmarks Bar" // as the workspace names it: the file keeps the browser's own name
	other.Children = nil
	bar.Children = append(bar.Children, news, &store.Node{ID: "new-1", Kind: store.KindFolder, Title: "Reading",
		Children: []*store.Node{{ID: "new-2", Kind: store.KindBookmark, Title: "Blog", URL: "https://blog.example/"}}})
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	out, err := d.Encode(tr, now, guids())
	if err != nil {
		t.Fatal(err)
	}
	doc := decode(t, out)
	if find(t, doc, "1")["name"] != "Bookmarks bar" {
		t.Fatalf("the bar root was renamed: %v", find(t, doc, "1")["name"])
	}
	n8 := find(t, doc, "8")
	if n8["name"] != "Daily news" || n8["guid"] != "00000000-0000-4000-8000-000000000008" {
		t.Fatalf("moved and renamed node 8: %v", n8)
	}
	reading, blog := find(t, doc, "10"), find(t, doc, "11")
	if reading["name"] != "Reading" || reading["type"] != "folder" || blog["url"] != "https://blog.example/" || blog["guid"] != "10000000-0000-4000-8000-000000000002" {
		t.Fatalf("new nodes: %v %v", reading, blog)
	}
	if blog["date_added"] != "13439217600000000" {
		t.Fatalf("date_added %v, want now in Chromium's microseconds since 1601", blog["date_added"])
	}
	if sum, _ := Checksum(doc["roots"].(map[string]any)); doc["checksum"] != sum || sum == pythonChecksum {
		t.Fatalf("checksum not recomputed: %v", doc["checksum"])
	}
	if _, tr2, err := Read(out); err != nil || len(tr2.Root.Children[1].Children) != 0 {
		t.Fatalf("read back: %v %+v", err, tr2.Root.Children[1])
	}
}

func TestEncodeRefusesATargetThatDoesNotFitTheFile(t *testing.T) {
	cases := map[string]func(tr store.Tree){
		"a root swapped": func(tr store.Tree) {
			tr.Root.Children[0], tr.Root.Children[1] = tr.Root.Children[1], tr.Root.Children[0]
		},
		"a bookmark as folder": func(tr store.Tree) { tr.Root.Children[0].Children[0].Kind = store.KindFolder },
		"a node twice": func(tr store.Tree) {
			tr.Root.Children[1].Children = append(tr.Root.Children[1].Children, tr.Root.Children[0].Children[0])
		},
	}
	for name, spoil := range cases {
		d, tr, _ := fixture(t)
		spoil(tr)
		if _, err := d.Encode(tr, time.Now(), guids()); err == nil {
			t.Errorf("%s: encoded", name)
		}
	}
	// Mobile bookmarks left out of the target stays as it was.
	d, tr, _ := fixture(t)
	tr.Root.Children = tr.Root.Children[:2]
	out, err := d.Encode(tr, time.Now(), guids())
	if err != nil || find(t, decode(t, out), "9")["name"] != "Phone" {
		t.Fatalf("mobile: %v", err)
	}
}

func TestSyncOffWithoutSyncMetadata(t *testing.T) {
	d, _, err := Read([]byte(`{"checksum":"","roots":{"bookmark_bar":{"id":"1","name":"Bookmarks bar","type":"folder","children":[]},"other":{"id":"2","name":"Other bookmarks","type":"folder","children":[]},"synced":{"id":"3","name":"Mobile bookmarks","type":"folder","children":[]}},"version":1}`))
	if err != nil || d.SyncOn() {
		t.Fatalf("%v, sync on %v", err, d != nil && d.SyncOn())
	}
}

func TestRunningAndReplace(t *testing.T) {
	user := t.TempDir()
	profile := filepath.Join(user, "Default")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if Running(profile) {
		t.Fatal("running with no lock")
	}
	if err := os.Symlink("host-123", filepath.Join(user, "SingletonLock")); err != nil {
		t.Fatal(err)
	}
	if !Running(profile) {
		t.Fatal("a SingletonLock link (Chromium's, which points nowhere) was not seen")
	}
	path, keep := filepath.Join(profile, "Bookmarks"), filepath.Join(profile, "Bookmarks.before-write")
	if err := os.WriteFile(path, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, []byte("new"), keep); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	kept, _ := os.ReadFile(keep)
	info, _ := os.Stat(path)
	if string(got) != "new" || string(kept) != "old" || info.Mode().Perm() != 0o640 {
		t.Fatalf("file %q kept %q mode %v", got, kept, info.Mode().Perm())
	}
}

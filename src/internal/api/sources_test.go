package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

const chromeFile = `{"roots":{"bookmark_bar":{"type":"folder","name":"Bookmarks Bar","children":[
 {"type":"folder","name":"Dev","children":[{"type":"url","name":"Go","url":"https://www.go.dev/?utm_source=x"}]},
 {"type":"url","name":"=HYPERLINK(\"x\")","url":"https://news.example/"}]},
 "other":{"type":"folder","name":"Other bookmarks","children":[]}}}`

const htmlFile = `<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p><DT><H3>Bookmarks Bar</H3><DL><p>
<DT><A HREF="https://go.dev/" ADD_DATE="1600000000">Go &amp; friends</A>
<DT><A HREF="https://news.example">News</A>
<DT><A HREF="https://new.example/">New</A></DL><p></DL><p>`

func multipartBody(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for _, name := range []string{"Bookmarks", "firefox.html"} {
		if body, ok := files[name]; ok {
			fw, err := mw.CreateFormFile("file", name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = fw.Write([]byte(body))
		}
	}
	_ = mw.Close()
	return b.String(), mw.FormDataContentType()
}

func TestSourcesMergeFilesAndTrashTheCopies(t *testing.T) {
	h := newServer(t, "tok").Handler()
	if w := send(h, "POST", "/api/v1/workspaces", `{"id":"main","name":"All"}`); w.Code != 201 {
		t.Fatalf("create %d", w.Code)
	}
	body, ct := multipartBody(t, map[string]string{"Bookmarks": chromeFile, "firefox.html": htmlFile})
	w := send(h, "POST", "/api/v1/workspaces/main/sources", body, "Content-Type", ct)
	var out struct {
		Entry      store.Entry
		Added      int
		Duplicates int
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 201 || out.Added != 3 || out.Duplicates != 2 || out.Entry.Op != "merge" ||
		!strings.Contains(out.Entry.Summary, "Added 3 bookmarks from Bookmarks, firefox.html; 2 duplicate copies went to the Trash") {
		t.Fatalf("sources %d %s", w.Code, w.Body)
	}
	tr, _ := tree(t, h)
	if len(tr.Trash) != 2 {
		t.Fatalf("trash %d", len(tr.Trash))
	}
	// The Go copy in Dev is deeper, so it is kept, with the earlier date of the other copy.
	var kept *store.Node
	for _, c := range tr.Root.Children[0].Children {
		if c.Title == "Dev" {
			kept = c.Children[0]
		}
	}
	if kept == nil || kept.Added != 1600000000 {
		t.Fatalf("kept %+v", kept)
	}

	w = send(h, "GET", "/api/v1/workspaces/main/duplicates.csv", "")
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if w.Code != 200 || err != nil || len(rows) != 3 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv %d %v %q", w.Code, err, w.Body)
	}
	byURL := map[string][]string{}
	for _, r := range rows[1:] {
		byURL[r[4]] = r
	}
	if r := byURL["https://go.dev/"]; r == nil || r[0] != "Go" || r[2] != "Bookmarks Bar/Dev" || r[5] != "Bookmarks Bar" || r[6] != "2020-09-13" {
		t.Fatalf("go row %q", r)
	}
	if r := byURL["https://news.example"]; r == nil || r[0] != `'=HYPERLINK("x")` {
		t.Fatalf("a title that looks like a formula must be defused: %q", r)
	}

	// A kept bookmark that later went to the Trash is still named, with where it is now.
	goID := kept.ID
	if w := send(h, "POST", "/api/v1/workspaces/main/trash", `{"ids":["`+goID+`"]}`); w.Code != 201 {
		t.Fatalf("trash kept %d %s", w.Code, w.Body)
	}
	if b := send(h, "GET", "/api/v1/workspaces/main/duplicates.csv", "").Body.String(); !strings.Contains(b, "Go,https://www.go.dev/?utm_source=x,(in the Trash)") {
		t.Fatalf("kept in the Trash: %s", b)
	}
	_ = send(h, "POST", "/api/v1/workspaces/main/undo", "")

	// Adding the same files again adds nothing new: the workspace's own bookmarks always win.
	w = send(h, "POST", "/api/v1/workspaces/main/sources?name=again.html", htmlFile)
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 201 || out.Added != 0 || out.Duplicates != 3 {
		t.Fatalf("again %d %s", w.Code, w.Body)
	}
}

func TestSourcesFromAnotherWorkspaceAndSeparate(t *testing.T) {
	h := newServer(t, "tok").Handler()
	for _, id := range []string{"a", "b"} {
		if w := send(h, "POST", "/api/v1/workspaces", `{"id":"`+id+`","name":"W `+id+`"}`); w.Code != 201 {
			t.Fatal(w.Code)
		}
	}
	if w := send(h, "POST", "/api/v1/workspaces/a/import", htmlFile); w.Code != 201 {
		t.Fatal(w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/b/sources?from=a&separate=1", ""); w.Code != 201 || !strings.Contains(w.Body.String(), `"added":3`) {
		t.Fatalf("from %d %s", w.Code, w.Body)
	}
	w := send(h, "GET", "/api/v1/workspaces/b", "")
	if !strings.Contains(w.Body.String(), `"title":"workspace W a"`) {
		t.Fatalf("separate: %s", w.Body)
	}
	for url, code := range map[string]int{
		"/api/v1/workspaces/b/sources?from=b":    400,
		"/api/v1/workspaces/b/sources?from=nope": 404,
		"/api/v1/workspaces/b/sources":           400,
		"/api/v1/workspaces/nope/sources":        404,
	} {
		if w := send(h, "POST", url, ""); w.Code != code {
			t.Errorf("%s: %d, want %d (%s)", url, w.Code, code, w.Body)
		}
	}
	if w := send(h, "POST", "/api/v1/workspaces/b/sources?name=x", "not bookmarks"); w.Code != 400 || !strings.Contains(w.Body.String(), "x: ") {
		t.Errorf("bad file %d %s", w.Code, w.Body)
	}
}

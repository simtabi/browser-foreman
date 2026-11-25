package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	h := newServer(t, apiTok).Handler()
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

	// Adding the same files again adds nothing new: the workspace's own bookmarks always win, and copies the Trash
	// already holds as duplicates of the same kept bookmark are not added to it again (3 copies: 2 already there).
	w = send(h, "POST", "/api/v1/workspaces/main/sources?name=again.html", htmlFile)
	var again struct {
		AlreadyInTrash int `json:"already_in_trash"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &again)
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 201 || out.Added != 0 || out.Duplicates+again.AlreadyInTrash != 3 || again.AlreadyInTrash != 2 {
		t.Fatalf("again %d %s", w.Code, w.Body)
	}
}

func TestSourcesFromAnotherWorkspaceAndSeparate(t *testing.T) {
	h := newServer(t, apiTok).Handler()
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

func TestUpdateReadsTheSourcesAgain(t *testing.T) {
	h := newServer(t, apiTok).Handler()
	for _, id := range []string{"up", "main"} {
		if w := send(h, "POST", "/api/v1/workspaces", `{"id":"`+id+`","name":"`+id+`"}`); w.Code != 201 {
			t.Fatal(w.Code)
		}
	}
	if w := send(h, "POST", "/api/v1/workspaces/up/import", htmlFile); w.Code != 201 {
		t.Fatal(w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/sources?from=up&name=f.html", ""); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/sources?name=chrome", chromeFile); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w := send(h, "GET", "/api/v1/workspaces/main/sources", "")
	if !strings.Contains(w.Body.String(), `"kind":"workspace"`) || !strings.Contains(w.Body.String(), `"kind":"file"`) || strings.Contains(w.Body.String(), "body") {
		t.Fatalf("sources %s", w.Body)
	}
	// Nothing changed upstream: nothing to do.
	if w := send(h, "POST", "/api/v1/workspaces/main/update", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"entry":null`) {
		t.Fatalf("idle update %d %s", w.Code, w.Body)
	}
	// Upstream: one bookmark renamed, one added.
	up := func() store.Tree {
		w := send(h, "GET", "/api/v1/workspaces/up", "")
		var out struct{ Tree store.Tree }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out.Tree
	}()
	var newsID string
	for _, c := range up.Root.Children[0].Children {
		if c.Title == "News" {
			newsID = c.ID
		}
	}
	if w := send(h, "PATCH", "/api/v1/workspaces/up/nodes/"+newsID, `{"title":"Daily news"}`); w.Code != 200 && w.Code != 201 {
		t.Fatalf("rename %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/up/nodes", `{"parent":"`+up.Root.Children[0].ID+`","kind":"bookmark","title":"Later","url":"https://later.example/"}`); w.Code != 201 {
		t.Fatalf("add %d %s", w.Code, w.Body)
	}
	w = send(h, "GET", "/api/v1/workspaces/main/update", "")
	type change struct {
		URL      string
		NewTitle string `json:"new_title"`
	}
	var pv struct {
		Plan    struct{ New, Retitle, Gone []change }
		Missing []string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pv); err != nil || len(pv.Plan.New) != 1 || pv.Plan.New[0].URL != "https://later.example/" ||
		len(pv.Plan.Retitle) != 1 || pv.Plan.Retitle[0].NewTitle != "Daily news" || len(pv.Missing) != 0 {
		t.Fatalf("preview %s", w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/update", ""); w.Code != 201 || !strings.Contains(w.Body.String(), "Updated from source: 1 new, 1 retitled upstream") {
		t.Fatalf("apply %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/update", ""); !strings.Contains(w.Body.String(), `"entry":null`) {
		t.Fatalf("a second update did something: %s", w.Body)
	}
	tr, _ := tree(t, h)
	if s := titles(tr.Root); !strings.Contains(s, "Daily news") || !strings.Contains(s, "Later") {
		t.Fatalf("after update: %s", s)
	}
	// The source workspace gone: named as missing, nothing else breaks.
	if w := send(h, "POST", "/api/v1/workspaces/nobody/update", ""); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestSaveLinksMakesOneFolderInOneEntry(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	before := len(entries(t, h))
	body := `{"folder":"Research","links":[{"url":"https://a.example/","title":"  A   page "},{"url":"https://b.example/"}]}`
	w := send(h, "POST", "/api/v1/workspaces/main/save-links", body)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"path":"Saved tabs/Research"`) || !strings.Contains(w.Body.String(), `"type":"bookmark.created"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/save-links", body); !strings.Contains(w.Body.String(), `"path":"Saved tabs/Research (2)"`) {
		t.Fatalf("the name is not made unique: %s", w.Body)
	}
	if n := len(entries(t, h)); n != before+2 {
		t.Fatalf("%d entries, want one per save", n-before)
	}
	tr, _ := tree(t, h)
	if s := titles(tr.Root); !strings.Contains(s, "Saved tabs(Research(A page https://b.example/) Research (2)(") {
		t.Fatalf("tree %s", s)
	}
	w = send(h, "POST", "/api/v1/workspaces/main/save-links", `{"folder":"x","parent":"Bookmarks Bar/New∕one","links":[{"url":"https://c.example/"}]}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"path":"Bookmarks Bar/New/one/x"`) {
		t.Fatalf("parent path %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{
		`{"folder":"x","links":[]}`,
		`{"folder":"x","links":[{"url":"javascript:alert(1)"}]}`,
		`{"folder":"x","parent":"Trash","links":[{"url":"https://a.example/"}]}`,
	} {
		if w := send(h, "POST", "/api/v1/workspaces/main/save-links", bad); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
}

func entries(t *testing.T, h http.Handler) []store.Entry {
	t.Helper()
	var out struct{ History []store.Entry }
	if err := json.Unmarshal(send(h, "GET", "/api/v1/workspaces/main/history", "").Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.History
}

func TestSourcesFromAFileOnThisComputer(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	dir := t.TempDir()
	path := filepath.Join(dir, "Default", "Bookmarks")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(chromeFile), 0o600)
	w := send(h, "POST", "/api/v1/workspaces/main/sources?path="+url.QueryEscape(path), "")
	if w.Code != 201 || !strings.Contains(w.Body.String(), "from Default/Bookmarks") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if b := send(h, "GET", "/api/v1/workspaces/main/sources", "").Body.String(); !strings.Contains(b, `"kind":"path"`) || !strings.Contains(b, `"path":"`) {
		t.Fatalf("sources %s", b)
	}
	// The browser saves a new bookmark: Update finds it in the file.
	_ = os.WriteFile(path, []byte(strings.Replace(chromeFile, `"other":{"type":"folder","name":"Other bookmarks","children":[]}`,
		`"other":{"type":"folder","name":"Other bookmarks","children":[{"type":"url","name":"Fresh","url":"https://fresh.example/"}]}`, 1)), 0o600)
	if w := send(h, "POST", "/api/v1/workspaces/main/update", ""); w.Code != 201 || !strings.Contains(w.Body.String(), "1 new") {
		t.Fatalf("update %d %s", w.Code, w.Body)
	}
	// Gone from disk: named as missing, not an error.
	_ = os.Remove(path)
	if w := send(h, "GET", "/api/v1/workspaces/main/update", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "no file at") {
		t.Fatalf("missing %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/sources?path="+url.QueryEscape(filepath.Join(dir, "notes.txt")), ""); w.Code != 400 {
		t.Fatalf("a file not named like bookmarks: %d", w.Code)
	}
	// Owners only: an editor cannot make the server read its files.
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "ed", store.RoleEditor, p)
	_, ed := signIn(t, h, "ed", p)
	if w := ed.do(h, "POST", "/api/v1/workspaces/main/sources?path="+url.QueryEscape(path), "", true); w.Code != 403 {
		t.Fatalf("editor read a file: %d %s", w.Code, w.Body)
	}
	if w := ed.do(h, "GET", "/api/v1/profiles", "", false); w.Code != 403 {
		t.Fatalf("editor listed profiles: %d", w.Code)
	}
}

func TestTabNotesDeleteAndPurgeRoutes(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	if w := send(h, "PUT", "/api/v1/tabs/notes", `{"kind":"tab","key":"https://go.dev/","note":" read ","tags":["b","a","a"," "]}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"tags":["a","b"]`) {
		t.Fatalf("note %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/v1/tabs/notes", ""); !strings.Contains(w.Body.String(), `"note":"read"`) {
		t.Fatalf("notes %s", w.Body)
	}
	if w := send(h, "PUT", "/api/v1/tabs/notes", `{"kind":"bookmark","key":"x"}`); w.Code != 400 {
		t.Fatalf("bad kind %d", w.Code)
	}
	for body, code := range map[string]int{
		`{}`:                                  400,
		`{"everything":true}`:                 400,
		`{"ids":["a"],"before":"2026-11-01"}`: 400,
		`{"everything":true,"confirm":"DELETE ALL"}`: 200,
		`{"ids":["2026-11-01_000000"]}`:              200,
	} {
		if w := send(h, "POST", "/api/v1/tabs/snapshots/delete", body); w.Code != code {
			t.Errorf("%s: %d, want %d (%s)", body, w.Code, code, w.Body)
		}
	}
	if w := send(h, "POST", "/api/v1/tabs/purge", `{"level":"pages"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"level":"pages"`) {
		t.Fatalf("purge %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/tabs/purge", `{"level":"all"}`); w.Code != 400 {
		t.Fatalf("bad level %d", w.Code)
	}
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "ed", store.RoleEditor, p)
	_, ed := signIn(t, h, "ed", p)
	for _, path := range []string{"/api/v1/tabs/purge", "/api/v1/tabs/snapshots/delete"} {
		if w := ed.do(h, "POST", path, `{"level":"pages","before":"2026-11-01"}`, true); w.Code != 403 {
			t.Errorf("editor %s: %d", path, w.Code)
		}
	}
}

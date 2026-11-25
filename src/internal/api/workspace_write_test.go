package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func seeded(t *testing.T) *Server {
	t.Helper()
	s := newServer(t, apiTok)
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "main", "Chrome", time.Now())
	tree := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "bar", Kind: store.KindFolder, Title: "Bookmarks Bar", Children: []*store.Node{
			{ID: "go", Kind: store.KindBookmark, Title: "Go", URL: "https://go.dev/"},
			{ID: "dev", Kind: store.KindFolder, Title: "Dev"}}},
		{ID: "other", Kind: store.KindFolder, Title: "Other bookmarks"}}}}
	if _, err := s.Data.Commit(ctx, "main", 0, store.Change{Op: "import", Summary: "Imported 1", Tree: tree}, time.Now()); err != nil {
		t.Fatal(err)
	}
	return s
}

func send(h http.Handler, method, url, body string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.Host = host
	r.Header.Set("Authorization", "Bearer "+apiTok)
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func tree(t *testing.T, h http.Handler) (store.Tree, string) {
	t.Helper()
	w := send(h, "GET", "/api/v1/workspaces/main", "")
	var out struct{ Tree store.Tree }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Tree, w.Header().Get("ETag")
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

func TestEditsAreEntriesWithEvents(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	_, etag := tree(t, h)
	if etag != `"1"` {
		t.Fatalf("etag %q", etag)
	}
	w := send(h, "POST", "/api/v1/workspaces/main/nodes", `{"parent":"dev","kind":"bookmark","title":"MDN","url":"https://developer.mozilla.org/"}`, "If-Match", etag)
	if w.Code != 201 || w.Header().Get("ETag") != `"2"` {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	// A second change made on the same, now stale, head is refused rather than overwriting the first.
	if w := send(h, "PATCH", "/api/v1/workspaces/main/nodes/go", `{"title":"Golang"}`, "If-Match", etag); w.Code != 412 {
		t.Fatalf("stale If-Match: %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/changes", `{"base":1,"ops":[{"op":"trash","ids":["go"]}]}`); w.Code != 409 {
		t.Fatalf("stale base: %d", w.Code)
	}
	if w := send(h, "PATCH", "/api/v1/workspaces/main/nodes/go", `{"title":"Golang"}`); w.Code != 201 {
		t.Fatalf("update %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/move", `{"ids":["go"],"parent":"other"}`); w.Code != 201 {
		t.Fatalf("move %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/changes", `{"ops":[{"op":"create","parent":"bar","kind":"bookmark","url":"javascript:alert(1)"}]}`); w.Code != 400 {
		t.Fatalf("javascript: URL accepted: %d", w.Code)
	}
	tr, _ := tree(t, h)
	if got := titles(tr.Root); got != "Bookmarks Bar(Dev(MDN)) Other bookmarks(Golang)" {
		t.Fatal(got)
	}
	var types []string
	got, _ := s.Bus.Since(0)
	for _, e := range got {
		types = append(types, e.Type)
	}
	if got := strings.Join(types, ","); got != "bookmark.created,workspace.committed,bookmark.updated,workspace.committed,bookmark.moved,workspace.committed" {
		t.Fatal(got)
	}
	var h2 struct{ History []store.Entry }
	_ = json.Unmarshal(send(h, "GET", "/api/v1/workspaces/main/history", "").Body.Bytes(), &h2)
	if len(h2.History) != 4 || h2.History[3].Summary != `Moved "Golang"` {
		t.Fatalf("%+v", h2.History)
	}
}

func TestTrashUndoRedoRollback(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	if w := send(h, "POST", "/api/v1/workspaces/main/trash", `{"ids":["go"],"reason":"duplicate"}`); w.Code != 201 {
		t.Fatalf("trash %d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/v1/workspaces/main/trash", ""); !strings.Contains(w.Body.String(), `"reason":"duplicate"`) {
		t.Fatal(w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/trash/restore", `{"ids":["go"]}`); w.Code != 201 {
		t.Fatalf("restore %d", w.Code)
	}
	send(h, "POST", "/api/v1/workspaces/main/trash", `{"ids":["go"]}`)
	if w := send(h, "DELETE", "/api/v1/workspaces/main/trash", ""); w.Code != 201 || !strings.Contains(w.Body.String(), "trash.emptied") {
		t.Fatalf("empty %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/undo", ""); w.Code != 201 {
		t.Fatalf("undo %d %s", w.Code, w.Body)
	}
	if tr, _ := tree(t, h); len(tr.Trash) != 1 {
		t.Fatal("undo did not bring the emptied Trash back")
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/redo", ""); w.Code != 201 {
		t.Fatalf("redo %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/rollback", `{"to":1}`); w.Code != 201 {
		t.Fatalf("rollback %d %s", w.Code, w.Body)
	}
	if tr, _ := tree(t, h); titles(tr.Root) != "Bookmarks Bar(Go Dev()) Other bookmarks()" || len(tr.Trash) != 0 {
		t.Fatal(titles(tr.Root))
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/rollback", `{"to":99}`); w.Code != 400 {
		t.Fatalf("rollback past head: %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/nope/undo", ""); w.Code != 404 {
		t.Fatalf("missing workspace: %d", w.Code)
	}
}

func TestCreateImportExport(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	if w := send(h, "POST", "/api/v1/workspaces", `{"id":"work","name":"Work"}`); w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces", `{"id":"work"}`); w.Code != 409 {
		t.Fatalf("duplicate %d", w.Code)
	}
	file := `<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p><DT><H3>Bookmarks Bar</H3><DL><p><DT><A HREF="https://go.dev/">Go &amp; friends</A></DL><p></DL><p>`
	if w := send(h, "POST", "/api/v1/workspaces/work/import?name=bookmarks.html", file); w.Code != 201 || !strings.Contains(w.Body.String(), "Imported 1 bookmarks in 1 folders from bookmarks.html") {
		t.Fatalf("import %d %s", w.Code, w.Body)
	}
	w := send(h, "GET", "/api/v1/workspaces/work/export", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<A HREF="https://go.dev/">Go &amp; friends</A>`) ||
		!strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("export html %d %v %s", w.Code, w.Header(), w.Body)
	}
	if w := send(h, "GET", "/api/v1/workspaces/work/export?format=chrome", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"bookmark_bar"`) {
		t.Fatalf("export chrome %d", w.Code)
	}
	if w := send(h, "GET", "/api/v1/workspaces/work/export?format=pdf", ""); w.Code != 400 {
		t.Fatalf("unknown format %d", w.Code)
	}
}

func TestViewersCannotEdit(t *testing.T) {
	s := seeded(t)
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "bob", store.RoleViewer, p)
	h := s.Handler()
	_, bob := signIn(t, h, "bob", p)
	if w := bob.do(h, "GET", "/api/v1/workspaces/main/export", "", false); w.Code != 200 {
		t.Fatalf("viewer export %d", w.Code)
	}
	if w := bob.do(h, "POST", "/api/v1/workspaces/main/trash", `{"ids":["go"]}`, true); w.Code != 403 {
		t.Fatalf("viewer edit %d", w.Code)
	}
}

func TestPutTreeDiffsAndGuards(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	tr, etag := tree(t, h)
	// Unchanged: no entry.
	body, _ := json.Marshal(tr)
	if w := send(h, "PUT", "/api/v1/workspaces/main/tree", string(body), "If-Match", etag); w.Code != 200 {
		t.Fatalf("no-op save %d %s", w.Code, w.Body)
	}
	// Move "go" into Dev and rename it, in one save.
	bar := tr.Root.Children[0]
	gonode, dev := bar.Children[0], bar.Children[1]
	bar.Children = bar.Children[1:]
	gonode.Title = "Golang"
	dev.Children = append(dev.Children, gonode)
	body, _ = json.Marshal(tr)
	w := send(h, "PUT", "/api/v1/workspaces/main/tree", string(body), "If-Match", etag)
	if w.Code != 201 || !strings.Contains(w.Body.String(), "bookmark.moved") || !strings.Contains(w.Body.String(), "bookmark.updated") {
		t.Fatalf("save %d %s", w.Code, w.Body)
	}
	if w := send(h, "PUT", "/api/v1/workspaces/main/tree", string(body), "If-Match", etag); w.Code != 412 {
		t.Fatalf("stale save %d", w.Code)
	}
	gonode.URL = "javascript:alert(1)"
	body, _ = json.Marshal(tr)
	if w := send(h, "PUT", "/api/v1/workspaces/main/tree", string(body)); w.Code != 400 {
		t.Fatalf("javascript: URL through a whole-tree save: %d", w.Code)
	}
	var got struct {
		Can struct{ Undo, Redo *string }
	}
	_ = json.Unmarshal(send(h, "GET", "/api/v1/workspaces/main", "").Body.Bytes(), &got)
	if got.Can.Undo == nil || *got.Can.Undo != "Moved 1, Edited 1" || got.Can.Redo != nil {
		t.Fatalf("can %+v", got.Can)
	}
}

func TestLockedWorkspacesRefuseChanges(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	send(h, "PATCH", "/api/v1/workspaces/main/nodes/go", `{"title":"Golang"}`)
	if w := send(h, "PATCH", "/api/v1/workspaces/main", `{"name":"Chrome, cleaned","notes":"before the move","locked":true}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"locked":true`) {
		t.Fatalf("lock %d %s", w.Code, w.Body)
	}
	for _, c := range []struct{ method, path, body string }{
		{"PATCH", "/api/v1/workspaces/main/nodes/go", `{"title":"x"}`},
		{"POST", "/api/v1/workspaces/main/undo", ""},
		{"POST", "/api/v1/workspaces/main/trash", `{"ids":["go"]}`},
		{"PATCH", "/api/v1/workspaces/main", `{"name":"renamed while locked"}`},
		{"PATCH", "/api/v1/workspaces/main", `{"archived":true}`},
		{"PATCH", "/api/v1/workspaces/main", `{"color":"teal","name":"sneaked in with a colour"}`},
	} {
		if w := send(h, c.method, c.path, c.body); w.Code != 423 {
			t.Errorf("%s %s %s on a locked workspace: %d", c.method, c.path, c.body, w.Code)
		}
	}
	tr, _ := tree(t, h)
	body, _ := json.Marshal(tr)
	if w := send(h, "PUT", "/api/v1/workspaces/main/tree", strings.Replace(string(body), "Golang", "Changed", 1)); w.Code != 423 {
		t.Fatalf("whole-tree save on a locked workspace: %d", w.Code)
	}
	// Colour and icon only tell workspaces apart, so a locked one still takes them; bad values are refused.
	if w := send(h, "PATCH", "/api/v1/workspaces/main", `{"color":"Teal","icon":"ti-briefcase"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"color":"teal","icon":"briefcase"`) {
		t.Fatalf("looks on a locked workspace %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{`{"color":"chartreuse"}`, `{"icon":"<svg>"}`, `{"icon":"a--b"}`} {
		if w := send(h, "PATCH", "/api/v1/workspaces/main", bad); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := send(h, "PATCH", "/api/v1/workspaces/main", `{"locked":false}`); w.Code != 200 {
		t.Fatalf("unlock %d", w.Code)
	}
	if w := send(h, "PATCH", "/api/v1/workspaces/main", `{"archived":true,"name":" "}`); w.Code != 400 {
		t.Fatalf("blank name %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/undo", ""); w.Code != 201 {
		t.Fatalf("undo after unlock %d", w.Code)
	}
	var list struct{ Workspaces []store.Workspace }
	_ = json.Unmarshal(send(h, "GET", "/api/v1/workspaces", "").Body.Bytes(), &list)
	if list.Workspaces[0].Name != "Chrome, cleaned" || list.Workspaces[0].Notes != "before the move" {
		t.Fatalf("%+v", list.Workspaces)
	}
}

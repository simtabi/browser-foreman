package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func withUI(s *Server) *Server {
	s.UI = fstest.MapFS{
		"index.html":           {Data: []byte("<html><body>\n<script>\nconst DATA = /*DATA*/null;\nwindow.BF_DATA = DATA;\n</script>\n</body></html>")},
		"assets/js/app.js":     {Data: []byte("console.log(1)")},
		"assets/css/a.min.css": {Data: []byte("body{}")},
	}
	return s
}

func get(h http.Handler, url string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", url, nil)
	r.Host = host
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPageInLocalMode(t *testing.T) {
	s := withUI(seeded(t))
	// A title that tries to end the script it is embedded in.
	ctx := context.Background()
	tr, _, _ := s.Data.State(ctx, "main", 0)
	tr.Root.Children[0].Children[0].Title = "</script><script>alert(1)</script> "
	_, _ = s.Data.Commit(ctx, "main", 1, store.Change{Op: "edit", Summary: "x", Tree: tr}, time.Now())
	w := get(s.Handler(), "/", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `window.BF_V1 = {`) || !strings.Contains(body, `"token":"tok"`) {
		t.Fatalf("%d %s", w.Code, body)
	}
	if strings.Contains(body, "</script><script>alert") || strings.Contains(body, " ") {
		t.Fatal("a bookmark title broke out of the boot script")
	}
	csp := w.Header().Get("Content-Security-Policy")
	m := regexp.MustCompile(`'nonce-([A-Za-z0-9_-]+)'`).FindStringSubmatch(csp)
	if m == nil || strings.Count(body, `<script nonce="`+m[1]+`">`) != 2 || strings.Contains(csp, "unsafe-inline' 'nonce") {
		t.Fatalf("every inline script must carry this response's nonce: %s", csp)
	}
	if w2 := get(s.Handler(), "/", nil); strings.Contains(w2.Header().Get("Content-Security-Policy"), m[1]) {
		t.Fatal("the nonce was reused")
	}
	if w := get(s.Handler(), "/?ws=nope", nil); w.Code != 404 {
		t.Fatalf("missing workspace %d", w.Code)
	}
	if w := get(s.Handler(), "/assets/js/app.js", nil); w.Code != 200 {
		t.Fatalf("asset %d", w.Code)
	}
	for _, p := range []string{"/assets/", "/assets/js/", "/assets/../index.html", "/assets/nope.js"} {
		if w := get(s.Handler(), p, nil); w.Code != 404 && w.Code != 301 && w.Code != 307 {
			t.Fatalf("%s answered %d", p, w.Code)
		}
	}
	if w := get(s.Handler(), "/index.html", nil); w.Code != 404 {
		t.Fatalf("the raw template is served: %d", w.Code)
	}
}

func TestPageWithoutTokenOrUI(t *testing.T) {
	if w := get(withUI(newServer(t, "")).Handler(), "/", nil); w.Code != 503 || strings.Contains(w.Body.String(), "BF_V1") {
		t.Fatalf("no token: %d", w.Code)
	}
	unbuilt := newServer(t, "tok")
	unbuilt.UI = fstest.MapFS{}
	if w := get(unbuilt.Handler(), "/", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "make web") {
		t.Fatalf("unbuilt UI: %d %s", w.Code, w.Body)
	}
	if w := get(withUI(newServer(t, "tok")).Handler(), "/", nil); !strings.Contains(w.Body.String(), "browser-foreman import") {
		t.Fatalf("no workspaces: %s", w.Body)
	}
}

func TestPageInAccountsMode(t *testing.T) {
	s := withUI(seeded(t))
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "ada", store.RoleOwner, p)
	h := s.Handler()
	w := get(h, "/", nil)
	if !strings.Contains(w.Body.String(), "Sign in to Browser Foreman") || strings.Contains(w.Body.String(), "BF_V1") || strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("signed out: %s", w.Body)
	}
	_, sess := signIn(t, h, "ada", p)
	w = get(h, "/", sess.cookie)
	if !strings.Contains(w.Body.String(), `"csrf":"`+sess.csrf+`"`) || strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("signed in: %s", w.Body)
	}
}

func TestPageOpensTheFirstUnarchivedWorkspace(t *testing.T) {
	s := withUI(seeded(t))
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "zed", "Zed", time.Now())
	_, _ = s.Data.Commit(ctx, "zed", 0, store.Change{Op: "import", Summary: "x", Tree: store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder}}}, time.Now())
	main, _ := s.Data.Workspace(ctx, "main")
	main.Archived = true
	_ = s.Data.UpdateWorkspace(ctx, main)
	body := get(s.Handler(), "/", nil).Body.String()
	if !strings.Contains(body, `"workspace":{"archived":false,"head":1,"id":"zed"`) || !strings.Contains(body, `"workspaces":[`) {
		t.Fatalf("%s", body[strings.Index(body, "BF_V1"):][:300])
	}
}

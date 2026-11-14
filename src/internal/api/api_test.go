package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/events"
	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/tabs"
	"github.com/simtabi/browser-foreman/src/internal/webhooks"
)

func newServer(t *testing.T, token string) *Server {
	t.Helper()
	c := config.Defaults()
	c.APIToken = token
	st := webhooks.NewStore(t.TempDir())
	data, err := store.Open("json", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	checker := linkcheck.New(5 * time.Second)
	checker.AllowPrivate = true // tests check httptest servers on loopback
	checker.Limiter.Default = linkcheck.Rule{Concurrent: 4}
	checker.Patience = 100 * time.Millisecond // a rate-limited test site is not waited out for minutes
	bs := &browsers.Service{Platform: &fakeBrowsers{}}
	return &Server{Cfg: c, Bus: events.New(1000, time.Now), Hooks: st, Data: data, Now: time.Now, Checker: checker,
		Browsers: bs, Tabs: &tabs.Service{Data: data, Browsers: bs, Reader: &fakeTabs{}, Closer: &fakeTabs{}, Now: time.Now},
		Limit:    auth.NewLimiter(5, 15*time.Minute, 15*time.Minute),
		Dispatch: &webhooks.Dispatcher{Store: st, Client: http.DefaultClient, Attempts: 1, Backoff: time.Millisecond, Now: time.Now}}
}

func do(h http.Handler, method, url, body, host, origin, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealthAndHostOriginGuard(t *testing.T) {
	h := newServer(t, "tok").Handler()
	if w := do(h, "GET", "/api/v1/health", "", "127.0.0.1:8765", "", ""); w.Code != 200 || w.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("health %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/health", "", "evil.example:8765", "", ""); w.Code != 403 {
		t.Fatalf("rebinding host allowed: %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/version", "", "localhost:8765", "https://evil.example", ""); w.Code != 403 {
		t.Fatalf("cross-site origin allowed: %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/version", "", "localhost:8765", "http://localhost:8765", ""); w.Code != 200 {
		t.Fatalf("same origin refused: %d", w.Code)
	}
}

func TestWritesNeedTheToken(t *testing.T) {
	h := newServer(t, "tok").Handler()
	body := `{"url":"https://hooks.example.com/x","events":["*"],"secret":"0123456789abcdef"}`
	if w := do(h, "POST", "/api/v1/webhooks", body, "127.0.0.1:8765", "", ""); w.Code != 401 {
		t.Fatalf("no token: %d", w.Code)
	}
	if w := do(h, "POST", "/api/v1/webhooks", body, "127.0.0.1:8765", "", "nope"); w.Code != 401 {
		t.Fatalf("wrong token: %d", w.Code)
	}
	w := do(h, "POST", "/api/v1/webhooks", body, "127.0.0.1:8765", "", "tok")
	if w.Code != 201 || strings.Contains(w.Body.String(), "0123456789abcdef") {
		t.Fatalf("create %d %s (the secret must never be echoed)", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/v1/webhooks", "", "127.0.0.1:8765", "", "tok"); !strings.Contains(w.Body.String(), "hooks.example.com") || strings.Contains(w.Body.String(), "0123456789abcdef") {
		t.Fatalf("list %s", w.Body)
	}
	if w := do(h, "POST", "/api/v1/webhooks", `{"url":"http://169.254.169.254/","events":["*"],"secret":"0123456789abcdef"}`, "127.0.0.1:8765", "", "tok"); w.Code != 400 {
		t.Fatalf("metadata target: %d", w.Code)
	}
	if w := do(newServer(t, "").Handler(), "GET", "/api/v1/webhooks", "", "127.0.0.1:8765", "", "x"); w.Code != 503 {
		t.Fatalf("no token configured: %d", w.Code)
	}
}

func TestEventsStreamReplaysAndStreams(t *testing.T) {
	s := newServer(t, "tok")
	_, _ = s.Bus.Publish("a.one", nil)
	_, _ = s.Bus.Publish("a.two", nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/v1/events", nil)
	req.Header.Set("Last-Event-ID", "1")
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	go func() { time.Sleep(100 * time.Millisecond); _, _ = s.Bus.Publish("a.three", nil) }()
	sc := bufio.NewScanner(resp.Body)
	var got []string
	for sc.Scan() && len(got) < 2 {
		if v, ok := strings.CutPrefix(sc.Text(), "event: "); ok {
			got = append(got, v)
		}
	}
	if strings.Join(got, ",") != "a.two,a.three" {
		t.Fatalf("got %v: want the replayed a.two, then the live a.three", got)
	}
}

func TestWorkspacesAreReadOnlyAndNeedTheToken(t *testing.T) {
	s := newServer(t, "tok")
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "main", "Chrome", time.Now())
	tree := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "b1", Kind: store.KindBookmark, Title: "Go", URL: "https://go.dev/"}}}}
	_, _ = s.Data.Commit(ctx, "main", 0, store.Change{Op: "import", Summary: "Imported 1", Tree: tree}, time.Now())
	h := s.Handler()
	if w := do(h, "GET", "/api/v1/workspaces", "", "127.0.0.1:8765", "", ""); w.Code != 401 {
		t.Fatalf("bookmarks without the token: %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/workspaces", "", "127.0.0.1:8765", "", "tok"); w.Code != 200 || !strings.Contains(w.Body.String(), `"bookmarks":1`) {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/v1/workspaces/main", "", "127.0.0.1:8765", "", "tok"); w.Code != 200 || !strings.Contains(w.Body.String(), "https://go.dev/") {
		t.Fatalf("tree %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/v1/workspaces/main?at=9", "", "127.0.0.1:8765", "", "tok"); w.Code != 404 {
		t.Fatalf("missing entry %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/workspaces/nope/history", "", "127.0.0.1:8765", "", "tok"); w.Code != 404 {
		t.Fatalf("missing workspace %d", w.Code)
	}
	if w := do(h, "DELETE", "/api/v1/workspaces/main", "", "127.0.0.1:8765", "", "tok"); w.Code != 405 {
		t.Fatalf("workspaces are read-only here: %d", w.Code)
	}
}

func TestCrossSiteLoadsAreRefused(t *testing.T) {
	h := newServer(t, "tok").Handler()
	req := func(site, mode string) int {
		r := httptest.NewRequest("GET", "/api/v1/browsers/icon?name=Google%20Chrome", nil)
		r.Host = "127.0.0.1:8765"
		r.Header.Set("Sec-Fetch-Site", site)
		r.Header.Set("Sec-Fetch-Mode", mode)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := req("cross-site", "no-cors"); c != 403 {
		t.Fatalf("another site's <img> learned what this server holds: %d", c)
	}
	if c := req("same-origin", "no-cors"); c != 200 {
		t.Fatalf("the page's own <img>: %d", c)
	}
	if c := req("cross-site", "navigate"); c == 403 {
		t.Fatal("following a link to this server must still work")
	}
}

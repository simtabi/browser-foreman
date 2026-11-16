package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// fromExtension makes a request as the extension would: its own origin and its token.
func fromExtension(h http.Handler, method, url, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.Host = host
	r.Header.Set("Origin", "chrome-extension://abcdefghijklmnopabcdefghijklmnop")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.Header.Set("Sec-Fetch-Mode", "cors")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func paired(t *testing.T, s *Server, h http.Handler, browser string) string {
	t.Helper()
	var p struct{ Code string }
	_ = json.Unmarshal(send(h, "POST", "/api/v1/extension/pairings", "").Body.Bytes(), &p)
	w := fromExtension(h, "POST", "/api/v1/extension/pair", `{"code":"`+strings.ToLower(p.Code)+`","browser":"`+browser+`"}`, "")
	var out struct{ Token string }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 201 || !strings.HasPrefix(out.Token, "brofo_") {
		t.Fatalf("pair %d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/pair", `{"code":"`+p.Code+`","browser":"x"}`, ""); w.Code != 401 {
		t.Fatalf("a pairing code worked twice: %d", w.Code)
	}
	return out.Token
}

func TestPairingAndTabs(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	tok := paired(t, s, h, "Firefox")
	if u, err := s.Data.User(t.Context(), "local"); err != nil || u.PasswordHash != "" {
		t.Fatalf("local account %+v %v", u, err)
	}
	if w, _ := signIn(t, h, "local", ""); w.Code != 401 {
		t.Fatal("the local account signed in")
	}
	body := `{"windows":[{"id":3,"incognito":false,"tabs":[{"id":11,"url":"https://go.dev/","title":"Go","active":true,"groupId":5},
		{"id":12,"url":"https://a.example/","title":"A","groupId":-1}]},{"id":4,"incognito":true,"tabs":[{"id":13,"url":"https://private.example/","title":"P"}]}],
		"groups":[{"id":5,"windowId":3,"title":"Work","color":"blue"}]}`
	if w := fromExtension(h, "POST", "/api/v1/extension/tabs", body, tok); w.Code != 204 {
		t.Fatalf("push %d %s", w.Code, w.Body)
	}
	live := send(h, "GET", "/api/v1/tabs", "").Body.String()
	if !strings.Contains(live, `"browser":"Firefox"`) || !strings.Contains(live, `"title":"Work"`) || !strings.Contains(live, `"group":"5"`) || strings.Contains(live, "private.example") {
		t.Fatalf("%s", live)
	}
	// The config token is not an extension's: it cannot pretend to be one.
	if w := fromExtension(h, "POST", "/api/v1/extension/tabs", body, "tok"); w.Code != 401 {
		t.Fatalf("config token as an extension: %d", w.Code)
	}
	// An extension's token reads nothing it was not made for? It is a token of the local owner; revoking it ends it.
	var st struct{ Extensions []struct{ ID, Name string } }
	_ = json.Unmarshal(send(h, "GET", "/api/v1/extension/status", "").Body.Bytes(), &st)
	if len(st.Extensions) != 1 || st.Extensions[0].Name != "extension:Firefox" {
		t.Fatalf("%+v", st)
	}
	send(h, "DELETE", "/api/v1/tokens/"+st.Extensions[0].ID, "")
	if w := fromExtension(h, "POST", "/api/v1/extension/tabs", body, tok); w.Code != 401 {
		t.Fatalf("a revoked extension still reports: %d", w.Code)
	}
}

func TestWrongCodesAreLimited(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	for i := 0; i < 5; i++ {
		fromExtension(h, "POST", "/api/v1/extension/pair", `{"code":"AAAA-AAAA","browser":"x"}`, "")
	}
	var p struct{ Code string }
	_ = json.Unmarshal(send(h, "POST", "/api/v1/extension/pairings", "").Body.Bytes(), &p)
	if w := fromExtension(h, "POST", "/api/v1/extension/pair", `{"code":"`+p.Code+`","browser":"x"}`, ""); w.Code != 429 {
		t.Fatalf("after 5 wrong codes even the right one waits: %d", w.Code)
	}
	// Other sites still cannot call in: only extension origins pass.
	r := httptest.NewRequest("POST", "/api/v1/extension/pair", strings.NewReader(`{}`))
	r.Host = host
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("another site reached pairing: %d", w.Code)
	}
}

func TestCommandsReachTheirBrowser(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	chrome, firefox := paired(t, s, h, "Google Chrome"), paired(t, s, h, "Firefox")
	got := make(chan string, 1)
	go func() { got <- fromExtension(h, "GET", "/api/v1/extension/commands?wait=5", "", chrome).Body.String() }()
	time.Sleep(50 * time.Millisecond)
	w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Google Chrome","kind":"open-group","urls":["https://go.dev/"],"title":"Reading"}`)
	var q struct{ Command struct{ ID string } }
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	if w.Code != 202 {
		t.Fatalf("queue %d %s", w.Code, w.Body)
	}
	select {
	case b := <-got:
		if !strings.Contains(b, `"title":"Reading"`) {
			t.Fatal(b)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the waiting extension was not woken")
	}
	// A command still waiting for Chrome is not handed to Firefox, which asks first.
	send(h, "POST", "/api/v1/extension/commands", `{"browser":"Google Chrome","kind":"open","urls":["https://pkg.go.dev/"]}`)
	if b := fromExtension(h, "GET", "/api/v1/extension/commands", "", firefox).Body.String(); strings.Contains(b, "pkg.go.dev") {
		t.Fatal("Firefox got Chrome's command")
	}
	if b := fromExtension(h, "GET", "/api/v1/extension/commands", "", chrome).Body.String(); !strings.Contains(b, "pkg.go.dev") {
		t.Fatalf("Chrome lost its command: %s", b)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/commands/"+q.Command.ID+"/done", `{"ok":true}`, firefox); w.Code != 404 {
		t.Fatalf("another browser closed the command: %d", w.Code)
	}
	fromExtension(h, "POST", "/api/v1/extension/commands/"+q.Command.ID+"/done", `{"ok":true}`, chrome)
	if b := send(h, "GET", "/api/v1/extension/commands/"+q.Command.ID, "").Body.String(); !strings.Contains(b, `"state":"done"`) {
		t.Fatal(b)
	}
	for _, bad := range []string{`{"browser":"x","kind":"run","urls":["https://a/"]}`, `{"browser":"x","kind":"open","urls":["file:///etc/passwd"]}`} {
		if w := send(h, "POST", "/api/v1/extension/commands", bad); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
}

func TestReadBookmarksFromTheExtension(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	tok := paired(t, s, h, "Google Chrome")
	body := `{"nodes":[{"id":"0","title":"","children":[{"id":"1","title":"Bookmarks bar","children":[{"id":"5","title":"Go","url":"https://go.dev/"}]}]}]}`
	w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", body, tok)
	if w.Code != 201 || !strings.Contains(w.Body.String(), "Read 1 bookmarks in 1 folders from Google Chrome") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	tr, _, err := s.Data.State(t.Context(), "google-chrome-live", 0)
	if err != nil || tr.Root.Children[0].Children[0].ID != "ext-5" {
		t.Fatalf("%+v %v", tr, err)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", body, tok); w.Code != 201 {
		t.Fatalf("second read %d", w.Code)
	}
	if h, _ := s.Data.History(t.Context(), "google-chrome-live"); len(h) != 2 {
		t.Fatalf("each read is an entry: %d", len(h))
	}
	_ = store.RoleViewer
}

func TestAWriteCommandCarriesItsPlanAndReportsItsResult(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	chrome := paired(t, s, h, "Google Chrome")
	print := strings.Repeat("a", 64)
	for _, bad := range []string{
		`{"browser":"Google Chrome","kind":"write-bookmarks","expect":"` + print + `"}`,           // no workspace
		`{"browser":"Google Chrome","kind":"write-bookmarks","workspace":"google-chrome-target"}`, // no fingerprint
		`{"browser":"Google Chrome","kind":"write-bookmarks","workspace":"x","expect":"short"}`,   // not a fingerprint
	} {
		if w := send(h, "POST", "/api/v1/extension/commands", bad); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Google Chrome","kind":"write-bookmarks","workspace":"google-chrome-target","expect":"`+print+`"}`)
	var q struct{ Command struct{ ID string } }
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	if w.Code != 202 {
		t.Fatalf("queue %d %s", w.Code, w.Body)
	}
	if b := fromExtension(h, "GET", "/api/v1/extension/commands", "", chrome).Body.String(); !strings.Contains(b, `"workspace":"google-chrome-target"`) || !strings.Contains(b, `"expect":"`+print+`"`) {
		t.Fatalf("the extension did not get the plan: %s", b)
	}
	fromExtension(h, "POST", "/api/v1/extension/commands/"+q.Command.ID+"/done", `{"ok":true,"result":{"created":3,"moved":2}}`, chrome)
	if b := send(h, "GET", "/api/v1/extension/commands/"+q.Command.ID, "").Body.String(); !strings.Contains(b, `"result":{"created":3,"moved":2}`) {
		t.Fatalf("the result was not kept: %s", b)
	}
	// After writing, the extension reads the browser back into the plan it wrote; any other workspace stays closed.
	nodes := `{"nodes":[{"id":"0","title":"","children":[{"id":"1","title":"Bookmarks bar","children":[]}]}],"workspace":"%s"}`
	if w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", fmt.Sprintf(nodes, "google-chrome-target"), chrome); w.Code >= 300 {
		t.Fatalf("reading back into its write target: %d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", fmt.Sprintf(nodes, "main-ws"), chrome); w.Code != 403 {
		t.Fatalf("reading into a workspace that is not its own: %d", w.Code)
	}
}

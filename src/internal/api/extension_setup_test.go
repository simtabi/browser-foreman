package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestOneClickPairing(t *testing.T) {
	s := newServer(t, apiTok)
	var console bytes.Buffer
	s.Console = &console
	h := s.Handler()
	// Only an extension's own origin may ask; a page or a program cannot.
	if w := send(h, "POST", "/api/v1/extension/requests", `{"browser":"Google Chrome","version":"0.1.0"}`); w.Code != 403 {
		t.Fatalf("a non-extension asked to connect: %d", w.Code)
	}
	ask := func(browser string) (id, poll, match string) {
		w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"`+browser+`","version":"0.1.0"}`, "")
		var out struct{ ID, Poll, Match string }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != 201 || out.ID == "" || out.Poll == "" || len(out.Match) != 4 {
			t.Fatalf("ask %d %s", w.Code, w.Body)
		}
		return out.ID, out.Poll, out.Match
	}
	pollAs := func(id, poll string) (int, map[string]string) {
		w := fromExtension(h, "POST", "/api/v1/extension/requests/"+id+"/poll", `{"poll":"`+poll+`"}`, "")
		out := map[string]string{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	id, poll, match := ask("Google Chrome")
	// The server's terminal says so, with the letters, so a request is seen with no page open.
	if !strings.Contains(console.String(), "Google Chrome's extension asks to connect, showing "+match) {
		t.Fatalf("console: %q", console.String())
	}
	if code, out := pollAs(id, poll); code != 200 || out["state"] != "pending" || out["token"] != "" {
		t.Fatalf("pending poll %d %v", code, out)
	}
	if code, _ := pollAs(id, "pr_wrong"); code != 404 {
		t.Fatalf("a wrong poll secret was answered: %d", code)
	}
	// The owner sees the request with the same match code, and approves it.
	listed := send(h, "GET", "/api/v1/extension/requests", "").Body.String()
	if !strings.Contains(listed, `"match":"`+match+`"`) || strings.Contains(listed, poll) {
		t.Fatalf("listing %s", listed)
	}
	// A poll while the answer is being made waits; it does not lose the request.
	s.ext.mu.Lock()
	s.ext.requests[id].State = "answering"
	s.ext.mu.Unlock()
	if code, out := pollAs(id, poll); code != 200 || out["state"] != "pending" {
		t.Fatalf("poll while answering %d %v", code, out)
	}
	s.ext.mu.Lock()
	s.ext.requests[id].State = "pending"
	s.ext.mu.Unlock()
	if w := send(h, "POST", "/api/v1/extension/requests/"+id+"/approve", ""); w.Code != 200 {
		t.Fatalf("approve %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/extension/requests/"+id+"/approve", ""); w.Code != 404 {
		t.Fatalf("a request was answered twice: %d", w.Code)
	}
	code, out := pollAs(id, poll)
	if code != 200 || out["state"] != "approved" || !strings.HasPrefix(out["token"], "brofo_") {
		t.Fatalf("approved poll %d %v", code, out)
	}
	if code, _ := pollAs(id, poll); code != 404 {
		t.Fatal("the token was handed over twice")
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/tabs", `{"windows":[],"version":"0.1.0"}`, out["token"]); w.Code != 204 {
		t.Fatalf("the new token does not work: %d", w.Code)
	}
	// Denied: no token.
	id2, poll2, _ := ask("Firefox")
	send(h, "POST", "/api/v1/extension/requests/"+id2+"/deny", "")
	if code, out := pollAs(id2, poll2); code != 200 || out["state"] != "denied" || out["token"] != "" {
		t.Fatalf("denied poll %d %v", code, out)
	}
	// At most five wait at once.
	for i := range maxRequests {
		ask(fmt.Sprint("Browser ", i))
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"One more"}`, ""); w.Code != 429 {
		t.Fatalf("a sixth waiting request was taken: %d", w.Code)
	}
	// Requests expire.
	now := time.Now().Add(requestTTL + time.Second)
	s.Now = func() time.Time { return now }
	if listed := send(h, "GET", "/api/v1/extension/requests", "").Body.String(); listed != `{"requests":[]}`+"\n" && listed != `{"requests":[]}` {
		t.Fatalf("expired requests still listed: %s", listed)
	}
}

func TestAnExtensionTokenIsLimitedToTheExtension(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	tok := paired(t, s, h, "Google Chrome")
	if w := send(h, "POST", "/api/v1/workspaces", `{"id":"work","name":"Work"}`); w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	for _, c := range []struct {
		method, url, body string
		want              int
	}{
		{"GET", "/api/v1/workspaces", "", 200},
		{"GET", "/api/v1/workspaces/work", "", 0}, // let through (a new workspace has no entry yet: 404)
		{"PATCH", "/api/v1/workspaces/work", `{"name":"Taken"}`, 403},
		{"POST", "/api/v1/tokens", `{"name":"mine"}`, 403},
		{"GET", "/api/v1/preferences", "", 403},
		{"PUT", "/api/v1/preferences", `{"autosave":false}`, 403},
		{"GET", "/api/v1/workspaces/work/history", "", 403},
	} {
		w := fromExtension(h, c.method, c.url, c.body, tok)
		if (c.want == 0 && w.Code == 403) || (c.want != 0 && w.Code != c.want) {
			t.Errorf("%s %s with an extension's token: %d, want %d (0: anything but 403)", c.method, c.url, w.Code, c.want)
		}
	}
	// An extension reads only into its own workspaces, or one read from its browser before.
	tree := `{"nodes":[{"id":"0","title":"","children":[{"id":"1","title":"Bookmarks Bar","children":[]}]}]}`
	if w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", strings.Replace(tree, `{"nodes"`, `{"workspace":"work","nodes"`, 1), tok); w.Code != 403 {
		t.Fatalf("an extension wrote over another workspace: %d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", tree, tok); w.Code >= 300 {
		t.Fatalf("its own workspace: %d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", strings.Replace(tree, `{"nodes"`, `{"workspace":"google-chrome-live","nodes"`, 1), tok); w.Code >= 300 {
		t.Fatalf("reading again into its own: %d %s", w.Code, w.Body)
	}
	other := paired(t, s, h, "Firefox")
	if w := fromExtension(h, "POST", "/api/v1/extension/bookmarks", strings.Replace(tree, `{"nodes"`, `{"workspace":"google-chrome-live","nodes"`, 1), other); w.Code != 403 {
		t.Fatalf("Firefox's extension wrote into Chrome's workspace: %d", w.Code)
	}
}

func TestCommandsDoNotPileUp(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	for i := range maxQueued {
		if w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Opera","kind":"open","urls":["https://go.dev/`+fmt.Sprint(i)+`"]}`); w.Code != 202 {
			t.Fatalf("command %d: %d", i, w.Code)
		}
	}
	if w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Opera","kind":"open","urls":["https://go.dev/"]}`); w.Code != 429 {
		t.Fatalf("command %d was queued for a browser nobody collects for: %d", maxQueued+1, w.Code)
	}
	later := time.Now().Add(commandTTL + time.Minute)
	s.Now = func() time.Time { return later }
	if w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Opera","kind":"open","urls":["https://go.dev/"]}`); w.Code != 202 {
		t.Fatalf("old commands were not forgotten: %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Firefox","kind":"reload","urls":["javascript:alert(1)"]}`); w.Code != 202 || strings.Contains(w.Body.String(), "javascript") {
		t.Fatalf("reload: %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Firefox","kind":"read-bookmarks","urls":["https://x.example/"]}`); w.Code != 202 || strings.Contains(w.Body.String(), "x.example") {
		t.Fatalf("read-bookmarks: %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Firefox","kind":"uninstall"}`); w.Code != 400 {
		t.Fatalf("an unknown command was queued: %d", w.Code)
	}
	s.ext.mu.Lock()
	n := len(s.ext.byID) - 2 // less the reload and the read
	s.ext.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d commands kept, want 1", n)
	}
}

func TestGuidedInstall(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	s.ExtensionRoot = t.TempDir()
	fake := s.Browsers.Platform.(*fakeBrowsers)
	if w := send(h, "POST", "/api/v1/extension/setup", `{"browser":"Google Chrome"}`); w.Code != 409 {
		t.Fatalf("not built yet: %d %s", w.Code, w.Body)
	}
	dir := filepath.Join(s.ExtensionRoot, "chromium")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	guide := send(h, "GET", "/api/v1/extension/guide", "").Body.String()
	if !strings.Contains(guide, `"browser":"Google Chrome"`) || !strings.Contains(guide, `"built":true`) || !strings.Contains(guide, `"page":"chrome://extensions"`) {
		t.Fatalf("guide %s", guide)
	}
	if w := send(h, "POST", "/api/v1/extension/setup", `{"browser":"Google Chrome"}`); w.Code != 200 {
		t.Fatalf("setup %d %s", w.Code, w.Body)
	}
	got := fmt.Sprint(fake.ran)
	if !strings.Contains(got, "open -a /Applications/Google Chrome.app chrome://extensions") || !strings.Contains(got, "open -R "+filepath.Join(dir, "manifest.json")) {
		t.Fatalf("ran %s", got)
	}
	if w := send(h, "POST", "/api/v1/extension/setup", `{"browser":"Netscape"}`); w.Code != 404 {
		t.Fatalf("a browser that is not installed: %d", w.Code)
	}
	// The extension's own report of its version shows in the guide.
	tok := paired(t, s, h, "Google Chrome")
	fromExtension(h, "POST", "/api/v1/extension/tabs", `{"windows":[],"version":"0.0.9"}`, tok)
	if guide := send(h, "GET", "/api/v1/extension/guide", "").Body.String(); !strings.Contains(guide, `"version":"0.0.9"`) {
		t.Fatalf("version not kept: %s", guide)
	}
}

func TestNobodyMakesATokenThatPassesForAnExtensions(t *testing.T) {
	s := accountsServer(t)
	p := pw(t)
	addUser(t, s, "vee", store.RoleViewer, p)
	h := s.Handler()
	_, sess := signIn(t, h, "vee", p)
	if w := sess.do(h, "POST", "/api/v1/tokens", `{"name":"mine"}`, true); w.Code != 201 {
		t.Fatalf("an ordinary token: %d %s", w.Code, w.Body)
	}
	// A viewer's token named like an extension's would let them push tabs as a browser.
	if w := sess.do(h, "POST", "/api/v1/tokens", `{"name":" extension:Google Chrome"}`, true); w.Code != 400 {
		t.Fatalf("a token named like an extension's was made: %d %s", w.Code, w.Body)
	}
}

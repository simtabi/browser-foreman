package api

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// fakeBrowsers stands in for the operating system: Chrome and Safari installed, Safari the default, and every launch
// recorded instead of run, so no test ever opens a window.
type fakeBrowsers struct {
	mu  sync.Mutex
	ran [][]string
}

func (f *fakeBrowsers) Installed() []browsers.Browser {
	var out []browsers.Browser
	for _, b := range browsers.Known {
		if b.Name == "Google Chrome" || b.Name == "Safari" {
			b.Path = "/Applications/" + b.Name + ".app"
			out = append(out, b)
		}
	}
	return out
}
func (f *fakeBrowsers) DefaultBundle() string { return "com.apple.Safari" }
func (f *fakeBrowsers) Icon(b browsers.Browser, _ int) ([]byte, error) {
	return []byte("\x89PNG " + b.Name), nil
}
func (f *fakeBrowsers) Launch(_ context.Context, argv []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = append(f.ran, argv)
	return nil
}

func TestBrowsersAndOpening(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	fake := s.Browsers.Platform.(*fakeBrowsers)
	w := send(h, "GET", "/api/v1/browsers", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"default":"Safari"`) || !strings.Contains(w.Body.String(), `"open_with":["System default"]`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := send(h, "GET", "/api/v1/browsers/icon?name=Google%20Chrome", ""); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("icon %d", w.Code)
	}
	if w := send(h, "GET", "/api/v1/browsers/icon?name=Netscape", ""); w.Code != 404 {
		t.Fatalf("unknown icon %d", w.Code)
	}
	// No browsers named: the preferences decide, and the default resolves to Safari.
	if w := send(h, "POST", "/api/v1/open", `{"urls":["https://go.dev/"]}`); w.Code != 200 {
		t.Fatalf("open %d %s", w.Code, w.Body)
	}
	if got := strings.Join(fake.ran[0], " "); got != "open -a /Applications/Safari.app https://go.dev/" {
		t.Fatal(got)
	}
	if w := send(h, "POST", "/api/v1/open", `{"urls":["file:///etc/passwd"]}`); w.Code != 400 {
		t.Fatalf("file: link %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/open", `{"urls":["https://go.dev/"],"how":"incognito","browsers":["Safari"]}`); w.Code != 502 {
		t.Fatalf("private window in Safari %d %s", w.Code, w.Body)
	}
	if len(fake.ran) != 1 {
		t.Fatalf("refused opens still ran: %q", fake.ran)
	}
	evs, _ := s.Bus.Since(0)
	for _, e := range evs {
		if b, _ := json.Marshal(e); strings.Contains(string(b), "go.dev") {
			t.Fatal("a link reached the event stream")
		}
	}
}

func TestPreferencesAndRoles(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	if w := send(h, "PUT", "/api/v1/preferences", `{"open_with":["Google Chrome","Safari"],"include_incognito":true}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var p store.Prefs
	_ = json.Unmarshal(send(h, "GET", "/api/v1/preferences", "").Body.Bytes(), &p)
	if len(p.OpenWith) != 2 || !p.IncludeIncognito || p.KeepDays != 14 {
		t.Fatalf("%+v", p)
	}
	if w := send(h, "PUT", "/api/v1/preferences", `{"interval_seconds":1}`); w.Code != 400 {
		t.Fatalf("bad interval %d", w.Code)
	}
	if w := send(h, "PUT", "/api/v1/preferences", `{"surprise":1}`); w.Code != 400 {
		t.Fatalf("unknown field %d", w.Code)
	}
	s.Cfg.Server.Mode = "accounts"
	pw1 := pw(t)
	addUser(t, s, "ed", store.RoleEditor, pw1)
	_, ed := signIn(t, h, "ed", pw1)
	if w := ed.do(h, "POST", "/api/v1/open", `{"urls":["https://go.dev/"]}`, true); w.Code != 403 {
		t.Fatalf("an editor opened windows on the server's computer: %d", w.Code)
	}
}

package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/tabs"
)

// fakeTabs answers for Google Chrome: one normal window and one private one. It never touches a real browser.
type fakeTabs struct{ closed []tabs.CloseItem }

func (f *fakeTabs) Read(_ context.Context, b browsers.Browser) tabs.Browser {
	if b.Name != "Google Chrome" {
		return tabs.Browser{Browser: b.Name}
	}
	return tabs.Browser{Browser: b.Name, Running: true, Windows: []tabs.Window{
		{Index: 1, ID: "7", Mode: "normal", Tabs: []tabs.Tab{{URL: "https://go.dev/", Title: "Go", ID: "1"}}},
		{Index: 2, ID: "8", Mode: "incognito", Tabs: []tabs.Tab{{URL: "https://private.example/", Title: "Secret", ID: "2"}}},
	}}
}

func (f *fakeTabs) Close(_ context.Context, _ string, items []tabs.CloseItem) (int, []string, error) {
	f.closed = append(f.closed, items...)
	return len(items), nil, nil
}

func TestTabsRoutes(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	if w := send(h, "GET", "/api/v1/tabs", ""); w.Code != 200 || strings.Contains(w.Body.String(), "private.example") || !strings.Contains(w.Body.String(), `"incognito_tabs":1`) {
		t.Fatalf("live %d %s", w.Code, w.Body)
	}
	w := send(h, "POST", "/api/v1/tabs/snapshots", `{}`)
	var saved tabs.Saved
	_ = json.Unmarshal(w.Body.Bytes(), &saved)
	if w.Code != 201 || !saved.Stored {
		t.Fatalf("take %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/tabs/snapshots", `{}`); w.Code != 200 || !strings.Contains(w.Body.String(), "unchanged") {
		t.Fatalf("unchanged %d %s", w.Code, w.Body)
	}
	// What tab history holds: the one snapshot; its private window's page is not kept (include_incognito is off).
	if w := send(h, "GET", "/api/v1/tabs/stored", ""); w.Code != 200 || w.Body.String() != `{"incognito_pages":0,"snapshots":1}`+"\n" {
		t.Fatalf("stored %d %q", w.Code, w.Body)
	}
	// A custom range on the Timeline: one that holds the snapshot lists it, one ending before it lists none, bad input is 400.
	if w := send(h, "GET", "/api/v1/tabs/timeline?from=2000-01-01T00:00:00Z", ""); !strings.Contains(w.Body.String(), saved.ID) {
		t.Fatalf("from 2000: %s", w.Body)
	}
	if w := send(h, "GET", "/api/v1/tabs/timeline?to=2000-01-01T00:00:00Z", ""); strings.Contains(w.Body.String(), saved.ID) || w.Code != 200 {
		t.Fatalf("to 2000: %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{"from=soon", "from=2026-11-10T10:00&to=2026-11-09T10:00"} {
		if w := send(h, "GET", "/api/v1/tabs/timeline?"+bad, ""); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := send(h, "GET", "/api/v1/tabs/snapshots", ""); !strings.Contains(w.Body.String(), saved.ID) || strings.Contains(w.Body.String(), "go.dev") {
		t.Fatalf("list must not carry pages: %s", w.Body)
	}
	if w := send(h, "GET", "/api/v1/tabs/snapshots/"+saved.ID, ""); !strings.Contains(w.Body.String(), "go.dev") || strings.Contains(w.Body.String(), "private.example") {
		t.Fatalf("one %s", w.Body)
	}
	if w := send(h, "POST", "/api/v1/tabs/snapshots/"+saved.ID+"/restore", `{}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("restore %d %s", w.Code, w.Body)
	}
	if got := strings.Join(s.Browsers.Platform.(*fakeBrowsers).ran[0], " "); got != "open -na /Applications/Google Chrome.app --args --new-window https://go.dev/" {
		t.Fatal(got)
	}
	if w := send(h, "POST", "/api/v1/tabs/close", `{"items":[{"browser":"Google Chrome","window_id":7,"tab_id":1,"url":"https://go.dev/"}]}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"closed":1`) {
		t.Fatalf("close %d %s", w.Code, w.Body)
	}
	evs, _ := s.Bus.Since(0)
	for _, e := range evs {
		if b, _ := json.Marshal(e); strings.Contains(string(b), "go.dev") || strings.Contains(string(b), "private.example") {
			t.Fatalf("a page reached the event stream: %s", b)
		}
	}
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "ed", store.RoleEditor, p)
	_, ed := signIn(t, h, "ed", p)
	for _, path := range []string{"/api/v1/tabs/snapshots/" + saved.ID + "/restore", "/api/v1/tabs/close"} {
		if w := ed.do(h, "POST", path, `{}`, true); w.Code != 403 {
			t.Errorf("%s as an editor: %d", path, w.Code)
		}
	}
}

func TestScheduledRecheckTakesOnlyStaleResults(t *testing.T) {
	s := newServer(t, "tok")
	ctx := context.Background()
	linkWorkspace(t, s, "https://a.example") // links https://a.example/a and /b
	old := time.Now().Add(-10 * 24 * time.Hour)
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{{URL: "https://a.example/a", Status: "ok", Checked: old}})
	s.recheckStale(ctx)
	var started string
	evs, _ := s.Bus.Since(0)
	for _, e := range evs {
		if e.Type == "linkcheck.scheduled" {
			b, _ := json.Marshal(e.Data)
			started = string(b)
		}
	}
	if !strings.Contains(started, `"due":1`) {
		t.Fatalf("only the stale result is due, never the link that was never checked: %q", started)
	}
	s.checks.mu.Lock()
	j := s.checks.jobs[s.checks.live]
	s.checks.mu.Unlock()
	if j != nil {
		j.Cancel()
		j.Wait()
	}
	p := store.Defaults()
	p.RecheckDays = 0
	_ = store.SavePrefs(ctx, s.Data, p)
	n := len(evs)
	s.recheckStale(ctx)
	evs2, _ := s.Bus.Since(0)
	for _, e := range evs2[n:] {
		if e.Type == "linkcheck.scheduled" {
			t.Fatal("re-checks are off but one started")
		}
	}
}

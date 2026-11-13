package api

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/history"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestHistoryRoutes(t *testing.T) {
	dir := t.TempDir()
	us := func(at time.Time) int64 { return (at.Unix() + 11644473600) * 1_000_000 }
	now := time.Now()
	mk := func(name string, rows ...any) string {
		p := filepath.Join(dir, name, "History")
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		db, err := sql.Open("sqlite", "file:"+p)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(`CREATE TABLE urls (id INTEGER PRIMARY KEY, url TEXT, title TEXT, visit_count INT, last_visit_time INT)`); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(rows); i += 3 {
			if _, err := db.Exec(`INSERT INTO urls (url, title, visit_count, last_visit_time) VALUES (?, ?, 1, ?)`, rows[i], rows[i+1], rows[i+2]); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	a := mk("a", "https://go.dev/", "Go", us(now.Add(-time.Hour)), "https://unbookmarked.example/", "New find", us(now.Add(-2*time.Hour)))
	b := mk("b", "https://go.dev/", "The Go site", us(now.Add(-time.Minute)), "https://old.example/", "Old", us(now.AddDate(0, 0, -90)))
	was := historyFiles
	historyFiles = func() []history.File {
		return []history.File{
			{Browser: "Google Chrome", Profile: "Default", Kind: history.Chromium, Path: a, State: "readable"},
			{Browser: "Brave Browser", Profile: "Default", Kind: history.Chromium, Path: b, State: "readable"},
			{Browser: "Safari", Kind: history.Safari, Path: "/x/History.db", State: "blocked"},
			{Browser: "Vivaldi", Kind: history.Chromium, Path: "/x/*/History", State: "missing"},
		}
	}
	t.Cleanup(func() { historyFiles = was })

	s := seeded(t)
	h := s.Handler()
	w := send(h, "GET", "/api/v1/history/access", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"granted":false`) || !strings.Contains(w.Body.String(), `"blocked":1`) {
		t.Fatalf("access %d %s", w.Code, w.Body)
	}
	w = send(h, "GET", "/api/v1/history?workspace=main", "")
	var out struct {
		Visits []struct {
			URL, Title, Browser string
			Visits              int
			Bookmarked          *bool
		}
		Read    int
		Skipped []struct{ Browser, Why string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(out.Visits) != 2 || out.Visits[0].URL != "https://go.dev/" || out.Visits[0].Visits != 2 || out.Visits[0].Title != "The Go site" || out.Visits[0].Browser != "Brave Browser" {
		t.Fatalf("one row per address, newest visit wins, older than 30 days left out: %+v", out.Visits)
	}
	if out.Visits[0].Bookmarked == nil || !*out.Visits[0].Bookmarked || *out.Visits[1].Bookmarked {
		t.Fatalf("bookmarked flags %+v", out.Visits)
	}
	if out.Read != 2 || len(out.Skipped) != 1 || out.Skipped[0].Browser != "Safari" {
		t.Fatalf("read %d skipped %+v", out.Read, out.Skipped)
	}
	// Last opened, per bookmark: both profiles' visits to go.dev added up, the newest wins; pages that are not
	// bookmarks (unbookmarked.example) are not listed; the answer is never cached.
	w = send(h, "GET", "/api/v1/workspaces/main/opened", "")
	var op struct {
		Opened map[string]struct {
			Last   time.Time
			Visits int
		}
		Read int
	}
	if err := json.Unmarshal(w.Body.Bytes(), &op); err != nil || w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("opened %d %s", w.Code, w.Body)
	}
	if g, ok := op.Opened["https://go.dev/"]; !ok || g.Visits != 2 || now.Sub(g.Last) > 2*time.Minute || len(op.Opened) != 1 || op.Read != 2 {
		t.Fatalf("opened %+v", op)
	}
	if w := send(h, "GET", "/api/v1/workspaces/nope/opened", ""); w.Code != 404 {
		t.Fatalf("unknown workspace: %d", w.Code)
	}
	if w := send(h, "GET", "/api/v1/history?q=find&days=365", ""); !strings.Contains(w.Body.String(), "unbookmarked") || strings.Contains(w.Body.String(), "go.dev") {
		t.Fatalf("search %s", w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("history must not be cached")
	}
	for _, bad := range []string{"days=0", "limit=99999", "workspace=nope", "from=yesterday", "from=2026-11-10T10:00&to=2026-11-09T10:00"} {
		if w := send(h, "GET", "/api/v1/history?"+bad, ""); w.Code < 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	// Never an event: history does not reach webhooks.
	evs, _ := s.Bus.Since(0)
	for _, e := range evs {
		if strings.Contains(e.Type, "history") {
			t.Fatalf("history published: %s", e.Type)
		}
	}
	if history.SettingsURL != "" {
		if w := send(h, "POST", "/api/v1/history/access/open-settings", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
			t.Fatalf("open settings %d %s", w.Code, w.Body)
		}
	}
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "ed", store.RoleEditor, p)
	_, ed := signIn(t, h, "ed", p)
	if w := ed.do(h, "GET", "/api/v1/history", "", false); w.Code != 403 {
		t.Fatalf("an editor read history: %d", w.Code)
	}
}

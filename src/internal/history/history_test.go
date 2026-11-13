package history

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeDB(t *testing.T, path string, stmts ...string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

var day = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func TestReadsEachKindNewestFirst(t *testing.T) {
	dir := t.TempDir()
	chrome := filepath.Join(dir, "Chrome", "Default", "History")
	us := func(t time.Time) int64 { return (t.Unix() + 11644473600) * 1_000_000 }
	makeDB(t, chrome, `CREATE TABLE urls (id INTEGER PRIMARY KEY, url TEXT, title TEXT, visit_count INT, last_visit_time INT)`)
	db, _ := sql.Open("sqlite", "file:"+chrome)
	_, _ = db.Exec(`INSERT INTO urls (url, title, visit_count, last_visit_time) VALUES (?, 'Go docs', 3, ?), (?, 'Old', 1, ?), ('chrome://settings', 'Settings', 9, ?), (?, 'News', 2, ?)`,
		"https://go.dev/doc/", us(day), "https://old.example/", us(day.AddDate(0, 0, -30)), us(day), "https://news.example/", us(day.Add(time.Hour)))
	db.Close()
	fox := filepath.Join(dir, "Firefox", "p", "places.sqlite")
	makeDB(t, fox, `CREATE TABLE moz_places (id INTEGER PRIMARY KEY, url TEXT, title TEXT, visit_count INT, last_visit_date INT)`)
	db, _ = sql.Open("sqlite", "file:"+fox)
	_, _ = db.Exec(`INSERT INTO moz_places (url, title, visit_count, last_visit_date) VALUES ('https://mdn.example/', 'MDN', 4, ?), ('https://never.example/', 'Bookmarked only', 0, NULL)`, day.UnixMicro())
	db.Close()
	saf := filepath.Join(dir, "Safari", "History.db")
	makeDB(t, saf, `CREATE TABLE history_items (id INTEGER PRIMARY KEY, url TEXT, visit_count INT)`,
		`CREATE TABLE history_visits (id INTEGER PRIMARY KEY, history_item INT, visit_time REAL, title TEXT)`)
	db, _ = sql.Open("sqlite", "file:"+saf)
	_, _ = db.Exec(`INSERT INTO history_items VALUES (1, 'https://apple.example/', 2)`)
	_, _ = db.Exec(`INSERT INTO history_visits VALUES (1, 1, ?, 'Old title'), (2, 1, ?, 'Apple')`, float64(day.Unix()-978307200-60), float64(day.Unix()-978307200))
	db.Close()

	ctx := context.Background()
	got, err := Read(ctx, File{Browser: "Chrome", Profile: "Default", Kind: Chromium, Path: chrome}, Query{Since: day.AddDate(0, 0, -7)})
	if err != nil || len(got) != 2 || got[0].URL != "https://news.example/" || !got[1].Last.Equal(day) || got[1].Visits != 3 {
		t.Fatalf("chrome %+v %v", got, err)
	}
	// A custom range: Until leaves out the newer visit, and Limit counts only pages inside the range.
	if got, _ := Read(ctx, File{Kind: Chromium, Path: chrome}, Query{Since: day.AddDate(0, 0, -7), Until: day, Limit: 1}); len(got) != 1 || got[0].URL != "https://go.dev/doc/" {
		t.Fatalf("until: %+v", got)
	}
	if got, _ := Read(ctx, File{Kind: Chromium, Path: chrome}, Query{Words: []string{"go"}}); len(got) != 1 {
		t.Fatalf("words: %+v", got)
	}
	got, err = Read(ctx, File{Kind: Firefox, Path: fox}, Query{})
	if err != nil || len(got) != 1 || got[0].Title != "MDN" || !got[0].Last.Equal(day) {
		t.Fatalf("firefox %+v %v", got, err)
	}
	got, err = Read(ctx, File{Kind: Safari, Path: saf}, Query{})
	if err != nil || len(got) != 1 || got[0].Title != "Apple" || !got[0].Last.Equal(day) || got[0].Visits != 2 {
		t.Fatalf("safari %+v %v", got, err)
	}
	if b, _ := os.ReadFile(chrome); len(b) == 0 {
		t.Fatal("the browser's file was touched")
	}
}

func TestFindReportsBlockedAndMissing(t *testing.T) {
	dir := t.TempDir()
	makeDB(t, filepath.Join(dir, "Chrome", "Default", "History"), `CREATE TABLE x (a)`)
	locked := filepath.Join(dir, "Safari")
	_ = os.MkdirAll(locked, 0o700)
	_ = os.WriteFile(filepath.Join(locked, "History.db"), []byte("x"), 0o600)
	_ = os.Chmod(locked, 0)
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	got := find([]place{
		{"Chrome", Chromium, filepath.Join(dir, "Chrome", "*", "History")},
		{"Safari", Safari, filepath.Join(locked, "History.db")},
		{"Vivaldi", Chromium, filepath.Join(dir, "Vivaldi", "*", "History")},
	})
	if len(got) != 3 || got[0].State != "readable" || got[0].Profile != "Default" || got[1].State != "blocked" || got[2].State != "missing" {
		t.Fatalf("%+v", got)
	}
}

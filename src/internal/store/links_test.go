package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestLinkResults(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if got, _ := s.Links(ctx, []string{"https://a.example/"}); len(got) != 0 {
			t.Fatal("results before any check")
		}
		_ = s.SaveLinks(ctx, []LinkResult{{URL: "https://a.example/", Status: "ok", Code: 200, Checked: t0}, {URL: "https://b.example/", Status: "dead", Kind: "host", Checked: t0}})
		_ = s.SaveLinks(ctx, []LinkResult{{URL: "https://a.example/", Status: "moved", Final: "https://a.example/new", Checked: t0}})
		got, err := s.Links(ctx, []string{"https://a.example/", "https://nope.example/"})
		if err != nil || len(got) != 1 || got["https://a.example/"].Status != "moved" || got["https://a.example/"].Final != "https://a.example/new" || !got["https://a.example/"].Checked.Equal(t0) {
			t.Fatalf("%+v %v", got, err)
		}
		all, _ := s.Links(ctx, nil)
		if len(all) != 2 || all["https://b.example/"].Kind != "host" {
			t.Fatalf("%+v", all)
		}
		// More addresses than one query may bind.
		many := make([]LinkResult, 1200)
		urls := make([]string, 1200)
		for i := range many {
			urls[i] = fmt.Sprintf("https://x.example/%d", i)
			many[i] = LinkResult{URL: urls[i], Status: "ok", Checked: t0}
		}
		_ = s.SaveLinks(ctx, many)
		if got, err := s.Links(ctx, urls); err != nil || len(got) != 1200 {
			t.Fatalf("%d %v", len(got), err)
		}
	})
}

func TestLinkResultsKeepConfirmationFramingMetaAndArchive(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		in := LinkResult{URL: "https://gone.example/", Status: "dead", Kind: "page", Checked: t0.Add(25 * time.Hour),
			DeadSince: t0, Confirmed: true, Frame: "refused", Archive: "https://web.archive.org/web/2020/x",
			Meta: &PageMeta{Title: "T", Description: "D", Image: "https://gone.example/i.png", Canonical: "https://gone.example/", Site: "S"}}
		if err := s.SaveLinks(ctx, []LinkResult{in}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Links(ctx, []string{in.URL})
		r := got[in.URL]
		if err != nil || !r.DeadSince.Equal(t0) || !r.Confirmed || r.Frame != "refused" || r.Archive != in.Archive || r.Meta == nil || *r.Meta != *in.Meta {
			t.Fatalf("%+v %+v %v", r, r.Meta, err)
		}
		// Clearing them clears them.
		_ = s.SaveLinks(ctx, []LinkResult{{URL: in.URL, Status: "ok", Checked: t0}})
		got, _ = s.Links(ctx, []string{in.URL})
		if r := got[in.URL]; !r.DeadSince.IsZero() || r.Confirmed || r.Frame != "" || r.Archive != "" || r.Meta != nil {
			t.Fatalf("not cleared: %+v", r)
		}
	})
}

// A database made before these columns existed gains them, keeps its rows, and reads them with the new fields empty.
func TestOldLinksTableGainsTheNewColumns(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "browser-foreman.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE links (url TEXT PRIMARY KEY, status TEXT NOT NULL, code INTEGER NOT NULL DEFAULT 0,
  final TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', checked TEXT NOT NULL);
INSERT INTO links (url, status, checked) VALUES ('https://old.example/', 'dead', '2026-11-01T00:00:00Z');`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Links(context.Background(), nil)
	if r := got["https://old.example/"]; err != nil || r.Status != "dead" || !r.DeadSince.IsZero() || r.Meta != nil {
		t.Fatalf("%+v %v", got, err)
	}
}

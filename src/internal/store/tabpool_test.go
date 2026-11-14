package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestSnapshotsListTheirPool(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for id, doc := range map[string]string{"a": `{"workspace":"work","browsers":[]}`, "b": `{"browsers":[]}`} {
			// The caller's Pool is ignored: the document names it.
			if err := s.SaveTabs(ctx, TabSnapshot{ID: id, At: t0, Reason: "auto", Pool: "wrong", Windows: json.RawMessage(doc)}); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := s.TabSnapshots(ctx, 0)
		if err != nil || len(rows) != 2 {
			t.Fatalf("%v %v", rows, err)
		}
		got := map[string]string{}
		for _, r := range rows {
			got[r.ID] = r.Pool
		}
		if got["a"] != "work" || got["b"] != "" {
			t.Fatalf("pools %v, want a=work, b=shared", got)
		}
	})
}

func TestOlderSnapshotRowsGetTheirPool(t *testing.T) {
	dir := t.TempDir()
	// A database from before the pool column: the tabs table without it, holding one work-pool snapshot.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "browser-foreman.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE tabs (id TEXT PRIMARY KEY, at TEXT NOT NULL, reason TEXT NOT NULL, windows BLOB NOT NULL)`,
		`INSERT INTO tabs VALUES ('old', '2026-11-01T00:00:00Z', 'auto', '{"workspace":"work"}')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.TabSnapshots(context.Background(), 0)
	if err != nil || len(rows) != 1 || rows[0].Pool != "work" {
		t.Fatalf("%+v %v: an older row did not get its pool", rows, err)
	}
}

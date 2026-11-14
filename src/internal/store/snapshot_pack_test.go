package store

import (
	"context"
	"fmt"
	"testing"
)

// Tree snapshots are stored compressed in SQLite, and rows written before that (plain JSON) still read.
func TestSQLiteSnapshotsAreCompressedAndOldOnesStillRead(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateWorkspace(ctx, "w", "w", t0); err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range 2000 {
		names = append(names, fmt.Sprintf("page-%04d", i))
	}
	big := tree(names...)
	e, err := s.Commit(ctx, "w", 0, Change{Op: "import", Tree: big}, t0)
	if err != nil {
		t.Fatal(err)
	}
	_, plain, _ := Hash(big)
	var stored []byte
	if err := s.db.QueryRowContext(ctx, `SELECT body FROM snapshots WHERE hash = ?`, e.Snap).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) < 2 || stored[0] != 0x1f || stored[1] != 0x8b || len(stored)*4 > len(plain) {
		t.Fatalf("stored %d bytes for %d of JSON; want gzip at under a quarter", len(stored), len(plain))
	}
	got, _, err := s.State(ctx, "w", 0)
	if err != nil || len(titles(got)) != 2000 {
		t.Fatalf("read back %d titles: %v", len(titles(got)), err)
	}
	// A row from before compression.
	if _, err := s.db.ExecContext(ctx, `UPDATE snapshots SET body = ? WHERE hash = ?`, plain, e.Snap); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.State(ctx, "w", 0)
	if err != nil || len(titles(got)) != 2000 {
		t.Fatalf("old plain row read back %d titles: %v", len(titles(got)), err)
	}
}

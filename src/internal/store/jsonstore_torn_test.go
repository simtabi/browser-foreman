package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A crash or a full disk can leave the last history line half written. That one workspace, and every other one,
// must still list, and the next commit must land on a line of its own instead of joining the fragment.
func TestATornHistoryLineIsRepairedNotFatal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := OpenJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := s.CreateWorkspace(ctx, id, id, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit(ctx, id, 0, Change{Op: "import", Tree: tree("x")}, t0); err != nil {
			t.Fatal(err)
		}
	}
	hist := filepath.Join(dir, "json", "workspaces", "a", "history.jsonl")
	f, err := os.OpenFile(hist, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":2,"at":"2026-11`); err != nil { // the write that never finished
		t.Fatal(err)
	}
	f.Close()

	ws, err := s.Workspaces(ctx)
	if err != nil || len(ws) != 2 {
		t.Fatalf("one torn line hid every workspace: %v %d", err, len(ws))
	}
	if ws[0].Head != 1 {
		t.Fatalf("head %d, want 1: the fragment is not an entry", ws[0].Head)
	}
	if _, err := s.Commit(ctx, "a", 1, Change{Op: "edit", Tree: tree("x", "y")}, t0); err != nil {
		t.Fatal(err)
	}
	h, err := s.History(ctx, "a")
	if err != nil || len(h) != 2 || h[1].Seq != 2 {
		t.Fatalf("after the next commit: %v %+v", err, h)
	}
}

// A middle line that does not parse is real damage, not a torn write: it is still reported.
func TestADamagedMiddleLineIsStillAnError(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, _ := OpenJSON(dir)
	_, _ = s.CreateWorkspace(ctx, "a", "a", t0)
	_, _ = s.Commit(ctx, "a", 0, Change{Op: "import", Tree: tree("x")}, t0)
	hist := filepath.Join(dir, "json", "workspaces", "a", "history.jsonl")
	b, _ := os.ReadFile(hist)
	if err := os.WriteFile(hist, append([]byte("{not json}\n"), b...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.History(ctx, "a"); err == nil {
		t.Fatal("a damaged middle line was ignored")
	}
}

// A workspace folder left without its workspace.json (a create that failed halfway) neither hides the others nor
// blocks creating that workspace again.
func TestAHalfMadeWorkspaceFolderIsNotFatal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, _ := OpenJSON(dir)
	if _, err := s.CreateWorkspace(ctx, "ok", "ok", t0); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "json", "workspaces", "half"), 0o700); err != nil {
		t.Fatal(err)
	}
	ws, err := s.Workspaces(ctx)
	if err != nil || len(ws) != 1 || ws[0].ID != "ok" {
		t.Fatalf("%v %+v", err, ws)
	}
	if _, err := s.CreateWorkspace(ctx, "half", "Half", t0); err != nil {
		t.Fatalf("creating it again: %v", err)
	}
	if _, err := s.CreateWorkspace(ctx, "ok", "again", t0); err == nil {
		t.Fatal("a real workspace was created twice")
	}
}

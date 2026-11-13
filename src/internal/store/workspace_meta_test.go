package store

import (
	"context"
	"errors"
	"testing"
)

func TestRenameNotesLockArchive(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		w, _ := s.CreateWorkspace(ctx, "main", "Chrome", t0)
		if _, err := s.Commit(ctx, "main", 0, Change{Op: "import", Summary: "x", Tree: tree("go")}, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Commit(ctx, "main", 1, Change{Op: "edit", Summary: "y", Tree: tree("go", "dev")}, t0); err != nil {
			t.Fatal(err)
		}
		w.Name, w.Notes, w.Locked, w.Archived, w.Color, w.Icon = "Chrome (cleaned)", "kept for reference", true, true, "teal", "briefcase"
		if err := s.UpdateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Workspace(ctx, "main")
		if got.Name != "Chrome (cleaned)" || got.Notes != "kept for reference" || !got.Locked || !got.Archived || got.Head != 2 || got.Color != "teal" || got.Icon != "briefcase" {
			t.Fatalf("%+v", got)
		}
		if all, _ := s.Workspaces(ctx); len(all) != 1 || !all[0].Locked || all[0].Color != "teal" || all[0].Icon != "briefcase" {
			t.Fatalf("%+v", all)
		}
		if _, err := s.Commit(ctx, "main", 2, Change{Op: "edit", Summary: "z", Tree: tree("go")}, t0); !errors.Is(err, ErrLocked) {
			t.Fatalf("a locked workspace took a change: %v", err)
		}
		if _, err := Undo(ctx, s, "main", t0); !errors.Is(err, ErrLocked) {
			t.Fatal("undo got past the lock")
		}
		got.Locked = false
		_ = s.UpdateWorkspace(ctx, got)
		if _, err := s.Commit(ctx, "main", 2, Change{Op: "edit", Summary: "z", Tree: tree("go")}, t0); err != nil {
			t.Fatal(err)
		}
		if err := s.UpdateWorkspace(ctx, Workspace{ID: "nope", Name: "x"}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
	})
}

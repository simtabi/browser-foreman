package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDeletedWorkspacesAndPurge(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		tree := Tree{Root: &Node{ID: "root", Kind: KindFolder, Children: []*Node{{ID: "b", Kind: KindBookmark, Title: "Go", URL: "https://go.dev/"}}}}
		for _, id := range []string{"keep", "gone"} {
			if _, err := s.CreateWorkspace(ctx, id, id, t0); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Commit(ctx, id, 0, Change{Op: "import", Tree: tree}, t0); err != nil { // the same tree: one snapshot
				t.Fatal(err)
			}
		}
		_ = s.AddSource(ctx, "gone", Source{ID: "f1", Kind: SourceFile, Added: t0}, []byte("x"))
		w, _ := s.Workspace(ctx, "gone")
		w.Deleted = t0.Add(time.Hour)
		if err := s.UpdateWorkspace(ctx, w); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.Workspace(ctx, "gone"); !got.Deleted.Equal(t0.Add(time.Hour)) || got.Head != 1 {
			t.Fatalf("deleted mark: %+v", got)
		}
		all, _ := s.Workspaces(ctx)
		if len(all) != 2 || all[0].ID != "gone" || all[0].Deleted.IsZero() {
			t.Fatalf("list %+v", all)
		}
		if err := s.PurgeWorkspace(ctx, "gone"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Workspace(ctx, "gone"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("purged workspace still there: %v", err)
		}
		if l, _ := s.SourceList(ctx, "gone"); len(l) != 0 {
			t.Fatal("its sources stayed")
		}
		if got, _, err := s.State(ctx, "keep", 0); err != nil || got.Root.Children[0].ID != "b" {
			t.Fatalf("the other workspace lost the shared tree: %v", err)
		}
		if err := s.PurgeWorkspace(ctx, "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%v", err)
		}
		// The id is free again.
		if _, err := s.CreateWorkspace(ctx, "gone", "again", t0); err != nil {
			t.Fatal(err)
		}
	})
}

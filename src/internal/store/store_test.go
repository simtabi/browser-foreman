package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Every driver must pass the same suite: one behaviour, two storage formats.
var drivers = []string{"json", "sqlite"}

func open(t *testing.T, driver string) Store {
	t.Helper()
	s, err := Open(driver, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func tree(titles ...string) Tree {
	bar := &Node{ID: "f1", Kind: KindFolder, Title: "Bookmarks Bar"}
	for i, title := range titles {
		bar.Children = append(bar.Children, &Node{ID: "b" + string(rune('a'+i)), Kind: KindBookmark, Title: title, URL: "https://" + title + ".example/"})
	}
	return Tree{Root: &Node{ID: "root", Kind: KindFolder, Children: []*Node{bar}}}
}

func titles(tr Tree) []string {
	var out []string
	for _, c := range tr.Root.Children[0].Children {
		out = append(out, c.Title)
	}
	return out
}

var t0 = time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC)

func each(t *testing.T, f func(t *testing.T, s Store)) {
	for _, d := range drivers {
		t.Run(d, func(t *testing.T) { f(t, open(t, d)) })
	}
}

func TestWorkspacesCommitAndReadBack(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if _, err := s.CreateWorkspace(ctx, "main", "Chrome cleanup", t0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.CreateWorkspace(ctx, "main", "again", t0); err == nil {
			t.Fatal("a second workspace with the same id was created")
		}
		if _, err := s.CreateWorkspace(ctx, "../evil", "x", t0); err == nil {
			t.Fatal("a path in an id was accepted")
		}
		e1, err := s.Commit(ctx, "main", 0, Change{Op: "import", Summary: "Imported 2 bookmarks", Tree: tree("a", "b")}, t0)
		if err != nil || e1.Seq != 1 {
			t.Fatalf("%+v %v", e1, err)
		}
		if _, err := s.Commit(ctx, "main", 0, Change{Op: "edit", Summary: "stale", Tree: tree("x")}, t0); !errors.Is(err, ErrConflict) {
			t.Fatalf("a change made on an old state: %v", err)
		}
		e2, err := s.Commit(ctx, "main", 1, Change{Op: "edit", Summary: "Added c", Tree: tree("a", "b", "c")}, t0.Add(time.Minute))
		if err != nil || e2.Seq != 2 {
			t.Fatalf("%+v %v", e2, err)
		}
		w, _ := s.Workspace(ctx, "main")
		if w.Head != 2 || w.Name != "Chrome cleanup" {
			t.Fatalf("%+v", w)
		}
		head, e, _ := s.State(ctx, "main", 0)
		old, _, _ := s.State(ctx, "main", 1)
		if len(titles(head)) != 3 || e.Seq != 2 || len(titles(old)) != 2 {
			t.Fatalf("head %v (entry %d), first %v", titles(head), e.Seq, titles(old))
		}
		if _, _, err := s.State(ctx, "main", 9); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing entry: %v", err)
		}
		all, _ := s.Workspaces(ctx)
		if len(all) != 1 || all[0].Head != 2 {
			t.Fatalf("%+v", all)
		}
	})
}

func TestInvalidTreesAreRefused(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		_, _ = s.CreateWorkspace(ctx, "w", "w", t0)
		dup := tree("a", "b")
		dup.Root.Children[0].Children[1].ID = dup.Root.Children[0].Children[0].ID
		for name, bad := range map[string]Tree{"no root": {}, "repeated id": dup,
			"bad kind": {Root: &Node{ID: "r", Kind: KindFolder, Children: []*Node{{ID: "x", Kind: "link"}}}}} {
			if _, err := s.Commit(ctx, "w", 0, Change{Op: "edit", Tree: bad}, t0); err == nil {
				t.Errorf("%s accepted", name)
			}
		}
	})
}

func TestUndoRedoAreEntriesToo(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		_, _ = s.CreateWorkspace(ctx, "w", "w", t0)
		_, _ = s.Commit(ctx, "w", 0, Change{Op: "import", Summary: "import", Tree: tree("a")}, t0)
		_, _ = s.Commit(ctx, "w", 1, Change{Op: "edit", Summary: "add b", Tree: tree("a", "b")}, t0)
		_, _ = s.Commit(ctx, "w", 2, Change{Op: "edit", Summary: "add c", Tree: tree("a", "b", "c")}, t0)
		state := func() []string { tr, _, _ := s.State(ctx, "w", 0); return titles(tr) }
		if _, err := Undo(ctx, s, "w", t0); err != nil || len(state()) != 2 {
			t.Fatalf("undo 1: %v %v", state(), err)
		}
		if _, err := Undo(ctx, s, "w", t0); err != nil || len(state()) != 1 {
			t.Fatalf("undo 2: %v %v", state(), err)
		}
		if _, err := Undo(ctx, s, "w", t0); !errors.Is(err, ErrNothing) {
			t.Fatalf("the import must not be undone: %v", err)
		}
		if _, err := Redo(ctx, s, "w", t0); err != nil || len(state()) != 2 {
			t.Fatalf("redo 1: %v %v", state(), err)
		}
		if _, err := Redo(ctx, s, "w", t0); err != nil || len(state()) != 3 {
			t.Fatalf("redo 2: %v %v", state(), err)
		}
		if _, err := Redo(ctx, s, "w", t0); !errors.Is(err, ErrNothing) {
			t.Fatalf("nothing left to redo: %v", err)
		}
		_, _ = Undo(ctx, s, "w", t0)
		w, _ := s.Workspace(ctx, "w")
		_, _ = s.Commit(ctx, "w", w.Head, Change{Op: "edit", Summary: "new branch", Tree: tree("a", "b", "z")}, t0)
		if _, err := Redo(ctx, s, "w", t0); !errors.Is(err, ErrNothing) {
			t.Fatalf("a new change must clear redo: %v", err)
		}
		h, _ := s.History(ctx, "w")
		if len(h) != 9 {
			t.Fatalf("every step is kept: %d entries", len(h))
		}
	})
}

func TestTabSnapshots(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, id := range []string{"s1", "s2", "s3"} {
			w, _ := json.Marshal([]map[string]any{{"browser": "Safari", "tabs": i + 1}})
			if err := s.SaveTabs(ctx, TabSnapshot{ID: id, At: t0.Add(time.Duration(i) * time.Minute), Reason: "auto", Windows: w}); err != nil {
				t.Fatal(err)
			}
		}
		list, _ := s.TabSnapshots(ctx, 2)
		if len(list) != 2 || list[0].ID != "s3" || list[0].Windows != nil {
			t.Fatalf("newest first, windows left out: %+v", list)
		}
		one, err := s.TabSnapshot(ctx, "s1")
		if err != nil || string(one.Windows) != `[{"browser":"Safari","tabs":1}]` {
			t.Fatalf("%s %v", one.Windows, err)
		}
		if _, err := s.TabSnapshot(ctx, "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
}

func TestMigrateCopiesEverythingBothWays(t *testing.T) {
	ctx := context.Background()
	src, dst, back := open(t, "sqlite"), open(t, "json"), open(t, "sqlite")
	_, _ = src.CreateWorkspace(ctx, "w", "Work", t0)
	_, _ = src.Commit(ctx, "w", 0, Change{Op: "import", Summary: "import", Tree: tree("a")}, t0)
	_, _ = src.Commit(ctx, "w", 1, Change{Op: "edit", Summary: "add b", Tree: tree("a", "b")}, t0.Add(time.Hour))
	_, _ = Undo(ctx, src, "w", t0.Add(2*time.Hour))
	_ = src.SaveTabs(ctx, TabSnapshot{ID: "s1", At: t0, Reason: "manual", Windows: json.RawMessage(`[]`)})
	for _, pair := range [][2]Store{{src, dst}, {dst, back}} {
		ws, n, err := Migrate(ctx, pair[0], pair[1])
		if err != nil || ws != 1 || n != 3 {
			t.Fatalf("migrate: %d %d %v", ws, n, err)
		}
	}
	h1, _ := src.History(ctx, "w")
	h2, _ := back.History(ctx, "w")
	if len(h2) != 3 || h2[2].Op != "undo" || h2[2].Undid != 2 || !h2[1].At.Equal(h1[1].At) || h2[1].Snap != h1[1].Snap {
		t.Fatalf("history changed on the way: %+v vs %+v", h2, h1)
	}
	if _, err := back.TabSnapshot(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Migrate(ctx, src, back); err == nil {
		t.Fatal("migrating onto an existing workspace must refuse")
	}
	if _, err := Redo(ctx, back, "w", t0); err != nil {
		t.Fatalf("undo/redo state survives the move: %v", err)
	}
}

func TestFilesArePrivate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	js, _ := OpenJSON(dir)
	_, _ = js.CreateWorkspace(ctx, "w", "w", t0)
	_, _ = js.Commit(ctx, "w", 0, Change{Op: "import", Tree: tree("a")}, t0)
	sq, err := OpenSQLite(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer sq.Close()
	for _, p := range []string{filepath.Join(dir, "json", "workspaces", "w", "history.jsonl"), filepath.Join(dir, "browser-foreman.sqlite3")} {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: %v %v (others must not read it)", p, st.Mode().Perm(), err)
		}
	}
}

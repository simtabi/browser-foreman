package store

import (
	"context"
	"errors"
	"testing"
)

func TestSourcesAreKeptPerWorkspace(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if err := s.AddSource(ctx, "nope", Source{ID: "a", Kind: SourceFile}, []byte("x")); err == nil {
			t.Fatal("a source for a workspace that does not exist")
		}
		_, _ = s.CreateWorkspace(ctx, "main", "Main", t0)
		_, _ = s.CreateWorkspace(ctx, "other", "Other", t0)
		if got, err := s.SourceList(ctx, "main"); err != nil || len(got) != 0 {
			t.Fatalf("%v %v", got, err)
		}
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(s.AddSource(ctx, "main", Source{ID: "f1", Name: "Bookmarks", Kind: SourceFile, SHA256: "ab", Count: 3, Added: t0}, []byte("file one")))
		must(s.AddSource(ctx, "main", Source{ID: "w-other", Name: "workspace Other", Kind: SourceWorkspace, From: "other", Added: t0}, nil))
		must(s.AddSource(ctx, "main", Source{ID: "p1", Name: "Chrome Default", Kind: SourcePath, Path: "/x/Bookmarks", Added: t0}, nil))
		must(s.AddSource(ctx, "main", Source{ID: "f1", Name: "Bookmarks", Kind: SourceFile, Count: 4, Added: t0}, nil)) // read again: moves last, body kept
		list, _ := s.SourceList(ctx, "main")
		if len(list) != 3 || list[0].ID != "w-other" || list[2].ID != "f1" || list[2].Count != 4 || list[1].Path != "/x/Bookmarks" {
			t.Fatalf("%+v", list)
		}
		if b, err := s.SourceBody(ctx, "main", "f1"); err != nil || string(b) != "file one" {
			t.Fatalf("%q %v", b, err)
		}
		if _, err := s.SourceBody(ctx, "main", "p1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a path source keeps no copy: %v", err)
		}
		if _, err := s.SourceBody(ctx, "other", "f1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("sources leak between workspaces: %v", err)
		}
		if err := s.AddSource(ctx, "main", Source{ID: "../x"}, nil); err == nil {
			t.Fatal("a source id that is a path was accepted")
		}
	})
}

func TestSourcesTravelInBackupsAndMigrations(t *testing.T) {
	ctx := context.Background()
	src := open(t, "sqlite")
	_, _ = src.CreateWorkspace(ctx, "main", "Main", t0)
	_, _ = src.Commit(ctx, "main", 0, Change{Op: "merge", Tree: Tree{Root: &Node{ID: "root", Kind: KindFolder}}}, t0)
	_ = src.AddSource(ctx, "main", Source{ID: "f1", Name: "Bookmarks", Kind: SourceFile, Added: t0}, []byte("kept bytes"))
	_ = src.AddSource(ctx, "main", Source{ID: "p1", Name: "Chrome", Kind: SourcePath, Path: "/x/Bookmarks", Added: t0}, nil)
	check := func(t *testing.T, s Store, id string) {
		t.Helper()
		list, err := s.SourceList(ctx, id)
		if err != nil || len(list) != 2 || list[1].Path != "/x/Bookmarks" {
			t.Fatalf("%+v %v", list, err)
		}
		if b, err := s.SourceBody(ctx, id, "f1"); err != nil || string(b) != "kept bytes" {
			t.Fatalf("%q %v", b, err)
		}
	}
	dst := open(t, "json")
	if _, _, err := Migrate(ctx, src, dst); err != nil {
		t.Fatal(err)
	}
	check(t, dst, "main")
	b, err := MakeBackup(ctx, src, t0)
	if err != nil {
		t.Fatal(err)
	}
	again := open(t, "json")
	_, _ = again.CreateWorkspace(ctx, "main", "Taken", t0)
	res, err := Restore(ctx, again, b, t0)
	if err != nil {
		t.Fatal(err)
	}
	check(t, again, res.Workspaces["main"])
}

package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestTabNotesAndDeletingSnapshots(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		must(s.SetTabNote(ctx, TabNote{Kind: NoteTab, Key: "https://go.dev/", Note: "read later", Tags: []string{"go"}, Updated: t0}))
		must(s.SetTabNote(ctx, TabNote{Kind: NoteWindow, Key: "Chrome|7", Note: "work", Updated: t0.Add(time.Minute)}))
		must(s.SetTabNote(ctx, TabNote{Kind: NoteTab, Key: "https://go.dev/", Note: "read now", Tags: []string{"go"}, Updated: t0.Add(2 * time.Minute)}))
		notes, err := s.TabNotes(ctx)
		if err != nil || len(notes) != 2 || notes[0].Note != "read now" || notes[0].Tags[0] != "go" || notes[1].Kind != NoteWindow {
			t.Fatalf("%+v %v", notes, err)
		}
		must(s.SetTabNote(ctx, TabNote{Kind: NoteWindow, Key: "Chrome|7"})) // empty: removed
		if notes, _ := s.TabNotes(ctx); len(notes) != 1 {
			t.Fatalf("an empty note was kept: %+v", notes)
		}
		for _, id := range []string{"2026-11-01_100000", "2026-11-02_100000"} {
			must(s.SaveTabs(ctx, TabSnapshot{ID: id, At: t0, Reason: "auto", Windows: json.RawMessage(`{}`)}))
		}
		n, err := s.DeleteTabs(ctx, []string{"2026-11-01_100000", "nope", "../x"})
		if err != nil || n != 1 {
			t.Fatalf("%d %v", n, err)
		}
		if _, err := s.TabSnapshot(ctx, "2026-11-01_100000"); err == nil {
			t.Fatal("a deleted snapshot is still there")
		}
		if left, _ := s.TabSnapshots(ctx, 0); len(left) != 1 {
			t.Fatalf("%d left", len(left))
		}
		must(s.Scrub(ctx))
		if notes, _ := s.TabNotes(ctx); len(notes) != 1 {
			t.Fatal("deleting snapshots took notes with it")
		}
	})
}

func TestNotesTravelInBackupsAndMigrations(t *testing.T) {
	ctx := context.Background()
	src := open(t, "sqlite")
	_ = src.SetTabNote(ctx, TabNote{Kind: NoteTab, Key: "https://go.dev/", Note: "read", Updated: t0})
	dst := open(t, "json")
	if _, _, err := Migrate(ctx, src, dst); err != nil {
		t.Fatal(err)
	}
	if n, _ := dst.TabNotes(ctx); len(n) != 1 {
		t.Fatal("migrate lost the note")
	}
	b, _ := MakeBackup(ctx, src, t0)
	again := open(t, "json")
	_ = again.SetTabNote(ctx, TabNote{Kind: NoteTab, Key: "https://go.dev/", Note: "mine", Updated: t0})
	if _, err := Restore(ctx, again, b, t0); err != nil {
		t.Fatal(err)
	}
	if n, _ := again.TabNotes(ctx); len(n) != 1 || n[0].Note != "mine" {
		t.Fatalf("restore replaced a note or lost it: %+v", n)
	}
	fresh := open(t, "json")
	_, _ = Restore(ctx, fresh, b, t0)
	if n, _ := fresh.TabNotes(ctx); len(n) != 1 || n[0].Note != "read" {
		t.Fatalf("restore lost the note: %+v", n)
	}
}

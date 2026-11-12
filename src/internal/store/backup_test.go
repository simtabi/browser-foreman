package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBackupRestoreReplacesNothing(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		w, _ := s.CreateWorkspace(ctx, "main", "Chrome", t0)
		_, _ = s.Commit(ctx, "main", 0, Change{Op: "import", Summary: "Imported", Tree: tree("go", "dev")}, t0)
		_, _ = s.Commit(ctx, "main", 1, Change{Op: "edit", Summary: "Trashed dev", Tree: tree("go")}, t0.Add(time.Minute))
		_, _ = s.Commit(ctx, "main", 2, Change{Op: "undo", Summary: "Undid", Tree: tree("go", "dev"), Undid: 2}, t0.Add(2*time.Minute))
		w.Head, w.Notes, w.Locked = 3, "before the move", true
		_ = s.UpdateWorkspace(ctx, w)
		_ = s.SaveLinks(ctx, []LinkResult{{URL: "https://go.example/", Status: "ok", Checked: t0}})
		_ = s.CreateUser(ctx, User{Name: "ada", Role: RoleOwner, PasswordHash: "$argon2id$secret-hash", Created: t0})

		b, err := MakeBackup(ctx, s, t0)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(b)
		if strings.Contains(string(raw), "secret-hash") || strings.Contains(string(raw), `"ada"`) {
			t.Fatal("a backup carried an account")
		}
		if len(b.Trees) != 2 {
			t.Fatalf("trees are stored once per content: %d", len(b.Trees))
		}
		var back Backup
		_ = json.Unmarshal(raw, &back)
		// Into the same store: the existing workspace is untouched, the backup's comes back beside it.
		_ = s.SaveLinks(ctx, []LinkResult{{URL: "https://go.example/", Status: "dead", Checked: t0.Add(time.Hour)}})
		res, err := Restore(ctx, s, back, t0)
		if err != nil {
			t.Fatal(err)
		}
		if res.Workspaces["main"] != "main-restored-1" || res.Entries != 3 || res.Links != 0 {
			t.Fatalf("%+v", res)
		}
		got, _ := s.Workspace(ctx, "main-restored-1")
		h, _ := s.History(ctx, "main-restored-1")
		if !got.Locked || got.Notes != "before the move" || got.Head != 3 || h[2].Op != "undo" || h[2].Undid != 2 || !h[1].At.Equal(t0.Add(time.Minute)) {
			t.Fatalf("%+v %+v", got, h)
		}
		if l, _ := s.Links(ctx, []string{"https://go.example/"}); l["https://go.example/"].Status != "dead" {
			t.Fatal("an older result from the backup replaced a newer one")
		}
		back.Format = "something-else"
		if _, err := Restore(ctx, s, back, t0); err == nil {
			t.Fatal("a file that is not a backup was restored")
		}
	})
}

package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
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
		_ = s.CreateUser(ctx, User{Name: "ada", Role: RoleOwner, PasswordHash: "$argon2id$" + rand.Text(), Created: t0})

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

func TestBackupCarriesTabsAndPrefs(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		_ = s.SaveTabs(ctx, TabSnapshot{ID: "2026-11-12_072137", At: t0, Reason: "auto", Windows: json.RawMessage(`{"browsers":[]}`)})
		p := Defaults()
		p.IncludeIncognito, p.IntervalSeconds = true, 120
		_ = SavePrefs(ctx, s, p)
		b, err := MakeBackup(ctx, s, t0)
		if err != nil || len(b.Tabs) != 1 || len(b.Prefs) == 0 {
			t.Fatalf("%d %d %v", len(b.Tabs), len(b.Prefs), err)
		}
		raw, _ := json.Marshal(b)
		var back Backup
		_ = json.Unmarshal(raw, &back)
		fresh, _ := Open("json", t.TempDir(), "")
		defer fresh.Close()
		res, err := Restore(ctx, fresh, back, t0)
		if err != nil || res.Tabs != 1 || !res.Prefs {
			t.Fatalf("%+v %v", res, err)
		}
		got, _ := LoadPrefs(ctx, fresh)
		if !got.IncludeIncognito || got.IntervalSeconds != 120 {
			t.Fatalf("%+v", got)
		}
		// Into a store with its own preferences and that snapshot already: nothing is replaced.
		mine := Defaults()
		mine.KeepDays = 3
		other, _ := Open("json", t.TempDir(), "")
		defer other.Close()
		_ = SavePrefs(ctx, other, mine)
		_ = other.SaveTabs(ctx, TabSnapshot{ID: "2026-11-12_072137", At: t0, Reason: "mine", Windows: json.RawMessage(`{}`)})
		res, _ = Restore(ctx, other, back, t0)
		kept, _ := LoadPrefs(ctx, other)
		snap, _ := other.TabSnapshot(ctx, "2026-11-12_072137")
		if res.Prefs || res.Tabs != 0 || kept.KeepDays != 3 || snap.Reason != "mine" {
			t.Fatalf("a restore replaced something: %+v %+v %s", res, kept, snap.Reason)
		}
	})
}

// failOn refuses every commit to one workspace id, as a full disk might partway through a restore.
type failOn struct {
	Store
	id string
}

func (f failOn) Commit(ctx context.Context, id string, base int, c Change, at time.Time) (Entry, error) {
	if id == f.id {
		return Entry{}, errors.New("disk full")
	}
	return f.Store.Commit(ctx, id, base, c, at)
}

// A restore that fails partway takes back the workspaces it made, so trying again does not leave half-restored copies.
func TestAFailedRestoreTakesBackWhatItMade(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, id := range []string{"a", "b"} {
			_, _ = s.CreateWorkspace(ctx, id, id, t0)
			_, _ = s.Commit(ctx, id, 0, Change{Op: "import", Tree: tree("x")}, t0)
		}
		b, err := MakeBackup(ctx, s, t0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Restore(ctx, failOn{s, "b-restored-1"}, b, t0); err == nil {
			t.Fatal("the restore did not fail")
		}
		ws, _ := s.Workspaces(ctx)
		if len(ws) != 2 {
			t.Fatalf("left behind: %+v", ws)
		}
		if _, err := Restore(ctx, s, b, t0); err != nil {
			t.Fatal(err)
		}
		ws, _ = s.Workspaces(ctx)
		names := []string{}
		for _, w := range ws {
			names = append(names, w.ID)
		}
		if len(ws) != 4 {
			t.Fatalf("the retry: %v", names)
		}
	})
}

package tabs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestPurgeAndDelete(t *testing.T) {
	ctx := context.Background()
	data, err := store.Open("sqlite", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.Local)
	s := &Service{Data: data, Now: func() time.Time { return now }}
	save := func(id string, at time.Time) {
		st := State{At: at, Browsers: []Browser{{Browser: "Chrome", Running: true, Windows: []Window{
			{Index: 1, Mode: "normal", Tabs: []Tab{{URL: "https://go.dev/", Title: "Go"}}},
			{Index: 2, Mode: "incognito", Tabs: []Tab{{URL: "https://secret.example/", Title: "Secret"}, {URL: "https://b.example/"}}},
		}}}}
		st.Summary = Summarise(st.Browsers)
		b, _ := json.Marshal(snapshotDoc{State: st})
		if err := data.SaveTabs(ctx, store.TabSnapshot{ID: id, At: at, Reason: "auto", Windows: b}); err != nil {
			t.Fatal(err)
		}
	}
	save("2026-11-10_120000", now.Add(-72*time.Hour))
	save("2026-11-13_110000", now.Add(-time.Hour))

	res, err := s.Purge(ctx, "pages", 24*time.Hour)
	if err != nil || res.Snapshots != 1 || res.Windows != 1 || res.Pages != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	old, _, _ := s.Snapshot(ctx, "2026-11-10_120000")
	w := old.Browsers[0].Windows[1]
	if len(w.Tabs) != 0 || !w.Redacted || w.TabCount != 2 || old.Summary.IncognitoTabs != 2 || len(old.Browsers[0].Windows[0].Tabs) != 1 {
		t.Fatalf("pages purge: %+v %+v", w, old.Summary)
	}
	raw, _ := data.TabSnapshot(ctx, "2026-11-10_120000")
	if strings.Contains(string(raw.Windows), "secret.example") {
		t.Fatal("a purged page is still stored")
	}
	if recent, _, _ := s.Snapshot(ctx, "2026-11-13_110000"); len(recent.Browsers[0].Windows[1].Tabs) != 2 {
		t.Fatal("a snapshot newer than the limit was purged")
	}
	if again, _ := s.Purge(ctx, "pages", 24*time.Hour); again.Snapshots != 0 {
		t.Fatalf("purging twice changed something: %+v", again)
	}
	res, _ = s.Purge(ctx, "windows", 0)
	all, _, _ := s.Snapshot(ctx, "2026-11-13_110000")
	if res.Snapshots != 2 || len(all.Browsers[0].Windows) != 1 || all.Summary.IncognitoWindows != 0 {
		t.Fatalf("windows purge: %+v %+v", res, all.Summary)
	}
	if _, err := s.Purge(ctx, "all", 0); err == nil {
		t.Fatal("an unknown level was accepted")
	}

	if n, err := s.DeleteSnapshots(ctx, "", nil, "2026-11-12", false); err != nil || n != 1 {
		t.Fatalf("before: %d %v", n, err)
	}
	if n, _ := s.DeleteSnapshots(ctx, "", nil, "", true); n != 1 {
		t.Fatalf("everything: %d", n)
	}
	if _, err := s.DeleteSnapshots(ctx, "", nil, "yesterday", false); err == nil {
		t.Fatal("a bad day was accepted")
	}
}

func TestDeleteAndPurgeKeepToOnePool(t *testing.T) {
	ctx := context.Background()
	data, err := store.Open("sqlite", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.Local)
	s := &Service{Data: data, Now: func() time.Time { return now }}
	save := func(id, pool string) {
		st := State{At: now.Add(-48 * time.Hour), Browsers: []Browser{{Browser: "Chrome", Running: true, Windows: []Window{
			{Index: 1, Mode: "incognito", Tabs: []Tab{{URL: "https://secret.example/"}}},
		}}}}
		b, _ := json.Marshal(snapshotDoc{State: st, Workspace: pool})
		if err := data.SaveTabs(ctx, store.TabSnapshot{ID: id, At: st.At, Reason: "auto", Windows: b}); err != nil {
			t.Fatal(err)
		}
	}
	save("2026-11-11_120000", "")
	save("2026-11-11_120001", "work")
	if res, _ := s.PurgePool(ctx, "work", "pages", 0); res.Snapshots != 1 {
		t.Fatalf("purge in one pool changed %d snapshots, want 1", res.Snapshots)
	}
	if shared, _, _ := s.Snapshot(ctx, "2026-11-11_120000"); len(shared.Browsers[0].Windows[0].Tabs) != 1 {
		t.Fatal("purging the work pool purged the shared pool")
	}
	for _, try := range []func() (int, error){
		func() (int, error) { return s.DeleteSnapshots(ctx, "work", nil, "", true) },
		func() (int, error) { return s.DeleteSnapshots(ctx, "work", []string{"2026-11-11_120000"}, "", false) },
		func() (int, error) { return s.DeleteSnapshots(ctx, "work", nil, "2026-11-13", false) },
	} {
		if _, err := try(); err != nil {
			t.Fatal(err)
		}
	}
	left, _ := data.TabSnapshots(ctx, 0)
	if len(left) != 1 || left[0].ID != "2026-11-11_120000" {
		t.Fatalf("left %+v, want only the shared pool's snapshot", left)
	}
}

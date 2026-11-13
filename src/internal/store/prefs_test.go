package store

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestPrefs(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		p, err := LoadPrefs(ctx, s)
		if err != nil || p.OpenWith[0] != "System default" || !p.Autosave || p.KeepDays != 14 || p.SnapshotWidth != 1280 || p.SnapshotHeight != 800 || p.SnapshotWaitSeconds != 8 || p.SnapshotDir != "" {
			t.Fatalf("defaults: %+v %v", p, err)
		}
		p.OpenWith, p.IncludeIncognito, p.RecheckDays = []string{"Safari", "Google Chrome"}, true, 0
		if err := SavePrefs(ctx, s, p); err != nil {
			t.Fatal(err)
		}
		got, _ := LoadPrefs(ctx, s)
		if len(got.OpenWith) != 2 || !got.IncludeIncognito || got.RecheckDays != 0 {
			t.Fatalf("%+v", got)
		}
		for _, bad := range []func(*Prefs){
			func(p *Prefs) { p.IntervalSeconds = 1 }, func(p *Prefs) { p.WatchSeconds = 0 }, func(p *Prefs) { p.OpenWith = nil },
			func(p *Prefs) { p.SaveMode = "always" }, func(p *Prefs) { p.RecheckDays = -1 },
			func(p *Prefs) { p.SnapshotWidth = 100 }, func(p *Prefs) { p.SnapshotHeight = 9000 }, func(p *Prefs) { p.SnapshotWaitSeconds = 0 },
			func(p *Prefs) { p.SnapshotDir = "relative/thumbs" }, func(p *Prefs) { p.SnapshotDir = "/a/../b" },
		} {
			q := got
			bad(&q)
			if SavePrefs(ctx, s, q) == nil {
				t.Errorf("accepted %+v", q)
			}
		}
	})
}

func TestConcurrentPrefUpdatesAllLand(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		const n = 24
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := UpdatePrefs(ctx, s, func(p *Prefs) error { p.TabsOwn = append(p.TabsOwn, fmt.Sprint("ws", i)); return nil }); err != nil {
					t.Error(err)
				}
			}()
		}
		wg.Wait()
		got, _ := LoadPrefs(ctx, s)
		if len(got.TabsOwn) != n {
			t.Fatalf("%d of %d concurrent updates kept: one saved over another", len(got.TabsOwn), n)
		}
		if _, err := UpdatePrefs(ctx, s, func(p *Prefs) error { p.TabsOwn = nil; return fmt.Errorf("no") }); err == nil {
			t.Fatal("a failed change was not reported")
		}
		if again, _ := LoadPrefs(ctx, s); len(again.TabsOwn) != n {
			t.Fatal("a change that failed was saved")
		}
	})
}

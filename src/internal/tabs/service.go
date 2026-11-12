package tabs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Service takes and keeps tab snapshots for the server.
type Service struct {
	Data     store.Store
	Browsers *browsers.Service
	Reader   Reader
	Closer   Closer
	Now      func() time.Time
}

// tracked lists the browsers whose tabs are read: the scriptable installed ones, narrowed by the preferences.
func (s *Service) tracked(p store.Prefs) []browsers.Browser {
	want := map[string]bool{}
	for _, n := range p.Tracked {
		want[n] = true
	}
	var out []browsers.Browser
	for _, b := range s.Browsers.Installed() {
		if b.Scriptable && (p.Tracked == nil || want[b.Name]) {
			out = append(out, b)
		}
	}
	return out
}

// Live reads the tracked browsers now, with private windows redacted unless the owner keeps them.
func (s *Service) Live(ctx context.Context) (State, store.Prefs, error) {
	p, err := store.LoadPrefs(ctx, s.Data)
	if err != nil {
		return State{}, p, err
	}
	return Redact(ReadAll(ctx, s.Reader, s.tracked(p), s.Now()), p.IncludeIncognito), p, nil
}

// Saved is what Take did.
type Saved struct {
	Stored bool   `json:"stored"`
	ID     string `json:"id,omitempty"`
	Why    string `json:"why,omitempty"`
	State  State  `json:"live"`
}

// snapshotDoc is what a snapshot stores: the state and its fingerprint.
type snapshotDoc struct {
	State
	Fingerprint string `json:"fingerprint"`
}

// Take reads the browsers and stores a snapshot, unless nothing changed since the last one (force stores anyway).
func (s *Service) Take(ctx context.Context, reason string, force bool) (Saved, error) {
	live, _, err := s.Live(ctx)
	if err != nil {
		return Saved{}, err
	}
	if !Readable(live) {
		return Saved{Why: "no browser windows could be read", State: live}, nil
	}
	fp := Fingerprint(live)
	if !force {
		if last, err := s.Data.TabSnapshots(ctx, 1); err == nil && len(last) == 1 {
			if prev, err := s.Data.TabSnapshot(ctx, last[0].ID); err == nil {
				var doc snapshotDoc
				if json.Unmarshal(prev.Windows, &doc) == nil && doc.Fingerprint == fp {
					return Saved{ID: prev.ID, Why: "unchanged since the last snapshot", State: live}, nil
				}
			}
		}
	}
	id := live.At.Local().Format("2006-01-02_150405")
	if _, err := s.Data.TabSnapshot(ctx, id); err == nil {
		return Saved{ID: id, Why: "a snapshot was just taken", State: live}, nil
	}
	body, err := json.Marshal(snapshotDoc{State: live, Fingerprint: fp})
	if err != nil {
		return Saved{}, err
	}
	if err := s.Data.SaveTabs(ctx, store.TabSnapshot{ID: id, At: live.At, Reason: reason, Windows: body}); err != nil {
		return Saved{}, err
	}
	return Saved{Stored: true, ID: id, State: live}, nil
}

// Snapshot reads one stored snapshot back.
func (s *Service) Snapshot(ctx context.Context, id string) (State, store.TabSnapshot, error) {
	snap, err := s.Data.TabSnapshot(ctx, id)
	if err != nil {
		return State{}, snap, err
	}
	var doc snapshotDoc
	if err := json.Unmarshal(snap.Windows, &doc); err != nil {
		return State{}, snap, fmt.Errorf("snapshot %s is unreadable: %w", id, err)
	}
	return doc.State, snap, nil
}

// Restore opens a snapshot's windows again (pick: which, by position; nil: all), each in its own new window of the
// browser it was in, private ones in a private window.
func (s *Service) Restore(ctx context.Context, id string, pick []int) ([]browsers.Opened, error) {
	st, _, err := s.Snapshot(ctx, id)
	if err != nil {
		return nil, err
	}
	plan := RestorePlan(st, pick)
	if len(plan) == 0 {
		return nil, errors.New("nothing to restore: no window in it has pages that can be reopened")
	}
	var out []browsers.Opened
	for _, w := range plan {
		how := browsers.NewWindow
		if w.Private {
			how = browsers.Private
		}
		res, err := s.Browsers.Open(ctx, w.URLs, how, []string{w.Browser})
		if err != nil {
			return out, err
		}
		out = append(out, res...)
	}
	return out, nil
}

// Closed is what CloseTabs did; UndoSnapshot holds the tabs as they were, so restoring it brings them back.
type Closed struct {
	Closed       int      `json:"closed"`
	Skipped      []string `json:"skipped"`
	UndoSnapshot string   `json:"undo_snapshot"`
}

// CloseTabs stores a snapshot first (always), then closes the tabs, browser by browser.
func (s *Service) CloseTabs(ctx context.Context, items []CloseItem) (Closed, error) {
	if len(items) == 0 || len(items) > 500 {
		return Closed{}, errors.New("give 1 to 500 tabs")
	}
	before, err := s.Take(ctx, "before-close", true)
	if err != nil {
		return Closed{}, err
	}
	out := Closed{UndoSnapshot: before.ID, Skipped: []string{}}
	for _, name := range SortedNames(items) {
		var its []CloseItem
		for _, it := range items {
			if it.Browser == name {
				its = append(its, it)
			}
		}
		n, skipped, err := s.Closer.Close(ctx, name, its)
		if err != nil {
			out.Skipped = append(out.Skipped, name+": "+err.Error())
			continue
		}
		out.Closed += n
		out.Skipped = append(out.Skipped, skipped...)
	}
	_, _ = s.Take(ctx, "after-close", false)
	return out, nil
}

// Autosave takes snapshots until ctx ends: on any change (checked every watch_seconds) or every interval_seconds,
// as the preferences say; it reads them again each round, so a change applies without a restart. onSaved hears each
// stored snapshot.
func (s *Service) Autosave(ctx context.Context, onSaved func(Saved)) {
	wait := 15 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		p, err := store.LoadPrefs(ctx, s.Data)
		if err != nil {
			continue
		}
		wait = time.Duration(p.WatchSeconds) * time.Second
		if p.SaveMode == "interval" {
			wait = time.Duration(p.IntervalSeconds) * time.Second
		}
		if !p.Autosave {
			continue
		}
		if saved, err := s.Take(ctx, "auto", false); err == nil && saved.Stored && onSaved != nil {
			onSaved(saved)
		}
	}
}

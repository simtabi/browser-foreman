package tabs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Purged says what a purge removed.
type Purged struct {
	Level     string `json:"level"`
	Snapshots int    `json:"snapshots"` // snapshots changed
	Windows   int    `json:"windows"`   // private windows touched
	Pages     int    `json:"pages"`     // pages removed
}

// Purge forgets private windows in saved snapshots. Level "pages" removes their pages and titles and keeps how many
// windows and tabs there were; "windows" removes the private windows entirely, counts included. Only snapshots taken
// before olderThan go (zero: all). Normal windows are never touched. The store then scrubs what it freed.
func (s *Service) Purge(ctx context.Context, level string, olderThan time.Duration) (Purged, error) {
	if level != "pages" && level != "windows" {
		return Purged{}, errors.New(`level: "pages" or "windows"`)
	}
	out := Purged{Level: level}
	list, err := s.Data.TabSnapshots(ctx, 0)
	if err != nil {
		return out, err
	}
	cutoff := s.Now().Add(-olderThan)
	for _, row := range list {
		if olderThan > 0 && !row.At.Before(cutoff) {
			continue
		}
		snap, err := s.Data.TabSnapshot(ctx, row.ID)
		if err != nil {
			return out, err
		}
		var doc snapshotDoc
		if err := json.Unmarshal(snap.Windows, &doc); err != nil {
			continue // unreadable: nothing of it can be shown either
		}
		changed := false
		for bi := range doc.Browsers {
			b := &doc.Browsers[bi]
			kept := b.Windows[:0]
			for _, w := range b.Windows {
				if w.Mode != "incognito" {
					kept = append(kept, w)
					continue
				}
				if level == "windows" {
					out.Windows++
					out.Pages += len(w.Tabs)
					changed = true
					continue
				}
				if len(w.Tabs) > 0 || w.Groups != nil {
					out.Windows++
					out.Pages += len(w.Tabs)
					if w.TabCount == 0 {
						w.TabCount = len(w.Tabs)
					}
					w.Tabs, w.Groups, w.Redacted = nil, nil, true
					changed = true
				}
				kept = append(kept, w)
			}
			b.Windows = kept
		}
		if !changed {
			continue
		}
		doc.Summary = Summarise(doc.Browsers)
		b, err := json.Marshal(doc)
		if err != nil {
			return out, err
		}
		snap.Windows = b
		if err := s.Data.SaveTabs(ctx, snap); err != nil {
			return out, fmt.Errorf("snapshot %s: %w", snap.ID, err)
		}
		out.Snapshots++
	}
	if out.Snapshots > 0 {
		return out, s.Data.Scrub(ctx)
	}
	return out, nil
}

// Stored counts what one pool of tab history holds (PoolFor): every snapshot kept (hidden ones too), and how many
// private-window pages are still stored in them (what Purge "pages" would remove).
func (s *Service) Stored(ctx context.Context, pool string) (snapshots, privatePages int, err error) {
	list, err := s.Data.TabSnapshots(ctx, 0)
	if err != nil {
		return 0, 0, err
	}
	for _, row := range list {
		doc, _, err := s.loadDoc(ctx, row.ID)
		if err != nil || doc.Workspace != pool {
			continue
		}
		snapshots++
		for _, b := range doc.Browsers {
			for _, w := range b.Windows {
				if w.Mode == "incognito" {
					privatePages += len(w.Tabs)
				}
			}
		}
	}
	return snapshots, privatePages, nil
}

// DeleteSnapshots deletes snapshots for good: the ids given, those taken before a day (YYYY-MM-DD, local time), or
// every one. Notes stay; they belong to addresses and windows, not to snapshots.
func (s *Service) DeleteSnapshots(ctx context.Context, ids []string, before string, everything bool) (int, error) {
	var pick []string
	switch {
	case everything:
		list, err := s.Data.TabSnapshots(ctx, 0)
		if err != nil {
			return 0, err
		}
		for _, x := range list {
			pick = append(pick, x.ID)
		}
	case before != "":
		day, err := time.ParseInLocation("2006-01-02", before, time.Local)
		if err != nil {
			return 0, errors.New("before: a day as YYYY-MM-DD")
		}
		list, err := s.Data.TabSnapshots(ctx, 0)
		if err != nil {
			return 0, err
		}
		for _, x := range list {
			if x.At.Before(day) {
				pick = append(pick, x.ID)
			}
		}
	default:
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				pick = append(pick, id)
			}
		}
	}
	n, err := s.Data.DeleteTabs(ctx, pick)
	if err != nil {
		return n, err
	}
	if n > 0 {
		return n, s.Data.Scrub(ctx)
	}
	return n, nil
}

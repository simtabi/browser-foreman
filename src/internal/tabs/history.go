package tabs

import (
	"context"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Row is a snapshot in a list: when, why, which browsers had windows, and its counts (no pages).
type Row struct {
	ID       string    `json:"id"`
	At       time.Time `json:"at"`
	Reason   string    `json:"reason"`
	Browsers []string  `json:"browsers"`
	Summary  Summary   `json:"summary"`
}

// only narrows a state to some browsers (nil: all) and recounts it.
func only(s State, names []string) State {
	if names == nil {
		return s
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var kept []Browser
	for _, b := range s.Browsers {
		if want[b.Browser] {
			kept = append(kept, b)
		}
	}
	s.Browsers, s.Summary = kept, Summarise(kept)
	return s
}

func rowOf(snap store.TabSnapshot, st State) Row {
	r := Row{ID: snap.ID, At: snap.At, Reason: snap.Reason, Browsers: []string{}, Summary: st.Summary}
	for _, b := range st.Browsers {
		if len(b.Windows) > 0 {
			r.Browsers = append(r.Browsers, b.Browser)
		}
	}
	return r
}

// Rows lists up to limit snapshots, newest first, for the given browsers (nil: all); a snapshot where none of them
// had a window is left out.
func (s *Service) Rows(ctx context.Context, limit int, names []string) ([]Row, error) {
	all, err := s.Data.TabSnapshots(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := []Row{}
	for _, x := range all {
		st, snap, err := s.Snapshot(ctx, x.ID)
		if err != nil {
			continue
		}
		st = only(st, names)
		if names != nil && st.Summary.Windows+st.Summary.IncognitoWindows == 0 {
			continue
		}
		out = append(out, rowOf(snap, st))
	}
	return out, nil
}

// Change is a page that opened or closed between two snapshots.
type Change struct {
	Mode  string `json:"mode"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// Step is a snapshot in the timeline, with what opened and closed since the one before it.
type Step struct {
	Row
	OpenedN int      `json:"opened_n"`
	ClosedN int      `json:"closed_n"`
	Opened  []Change `json:"opened"`
	Closed  []Change `json:"closed"`
}

type pageKey struct{ mode, url string }

func pages(st State) map[pageKey]string {
	out := map[pageKey]string{}
	for _, b := range st.Browsers {
		for _, w := range b.Windows {
			for _, t := range w.Tabs {
				out[pageKey{w.Mode, t.URL}] = t.Title
			}
		}
	}
	return out
}

// Timeline lists the snapshots of the last `days` days, oldest first, each with what opened and closed since the
// one before (compared by address, per mode; private pages appear only where they were kept). days 0 means every
// snapshot kept. At most `limit` pages are listed per step; the counts are complete.
func (s *Service) Timeline(ctx context.Context, days int, names []string, limit int) ([]Step, error) {
	all, err := s.Data.TabSnapshots(ctx, 0)
	if err != nil {
		return nil, err
	}
	var since time.Time // days 0: no lower bound
	if days > 0 {
		since = s.Now().Add(-time.Duration(days) * 24 * time.Hour)
	}
	// all is newest first: walk it backwards, starting from the last snapshot before the window.
	start := len(all) - 1
	for start > 0 && all[start-1].At.Before(since) {
		start--
	}
	var prev map[pageKey]string
	out := []Step{}
	for i := start; i >= 0; i-- {
		st, snap, err := s.Snapshot(ctx, all[i].ID)
		if err != nil {
			continue
		}
		st = only(st, names)
		cur := pages(st)
		if snap.At.Before(since) {
			prev = cur
			continue
		}
		if names != nil && st.Summary.Windows+st.Summary.IncognitoWindows == 0 {
			prev = cur
			continue
		}
		step := Step{Row: rowOf(snap, st), Opened: []Change{}, Closed: []Change{}}
		if prev != nil {
			for k, title := range cur {
				if _, ok := prev[k]; !ok {
					step.OpenedN++
					if len(step.Opened) < limit {
						step.Opened = append(step.Opened, Change{k.mode, k.url, title})
					}
				}
			}
			for k, title := range prev {
				if _, ok := cur[k]; !ok {
					step.ClosedN++
					if len(step.Closed) < limit {
						step.Closed = append(step.Closed, Change{k.mode, k.url, title})
					}
				}
			}
		}
		out = append(out, step)
		prev = cur
	}
	return out, nil
}

// FirstSeen says when each address was first open in a stored snapshot (normal windows, and private ones only where
// their pages were kept).
func (s *Service) FirstSeen(ctx context.Context, urls []string) (map[string]time.Time, error) {
	want := map[string]bool{}
	for _, u := range urls {
		want[u] = true
	}
	all, err := s.Data.TabSnapshots(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	for i := len(all) - 1; i >= 0; i-- { // oldest first, so the first sighting wins
		st, snap, err := s.Snapshot(ctx, all[i].ID)
		if err != nil {
			continue
		}
		for k := range pages(st) {
			if want[k.url] {
				if _, seen := out[k.url]; !seen {
					out[k.url] = snap.At
				}
			}
		}
	}
	return out, nil
}

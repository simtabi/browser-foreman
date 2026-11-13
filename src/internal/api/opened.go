package api

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/history"
	"github.com/simtabi/browser-foreman/src/internal/merge"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// opened says when each bookmark of a workspace was last opened, from the browsers' own history (every profile this
// computer lets it read), with how many visits: {opened: {url: {last, visits}}, read, skipped}. Addresses are matched
// the way merging matches them (merge.Key), so http/https and a trailing slash do not hide a visit. Like the rest of
// history it is read through copies, never stored, never an event, and the answer is not cached.
func (s *Server) opened(w http.ResponseWriter, r *http.Request) {
	x, err := s.Data.Workspace(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// Each bookmark's key, and the addresses it is answered under (each bookmark's own, not the visited form): two
	// bookmarks may share a key (http and https, a trailing slash), and both are answered.
	byKey := map[string][]string{}
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		for _, c := range n.Children {
			if c.Kind == store.KindFolder {
				walk(c)
			} else if c.URL != "" {
				k := merge.Key(c.URL)
				if !slices.Contains(byKey[k], c.URL) {
					byKey[k] = append(byKey[k], c.URL)
				}
			}
		}
	}
	walk(t.Root)
	out := map[string]*seen{}
	type skipped struct{ Browser, Profile, Why string }
	skip, read := []skipped{}, 0
	q := history.Query{Limit: 100_000, Want: func(u string) bool { _, ok := byKey[merge.Key(u)]; return ok }}
	for _, f := range historyFiles() {
		if f.State != "readable" {
			if f.State == "blocked" {
				skip = append(skip, skipped{f.Browser, f.Profile, blockedWhy})
			}
			continue
		}
		vs, err := history.Read(r.Context(), f, q)
		if err != nil {
			skip = append(skip, skipped{f.Browser, f.Profile, err.Error()})
			continue
		}
		read++
		creditVisits(out, byKey, vs)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"opened": out, "read": read, "skipped": skip})
}

// seen is one bookmark's visits: the latest, and how many in all.
type seen struct {
	Last   time.Time `json:"last"`
	Visits int       `json:"visits"`
}

// creditVisits adds visits to every bookmark address sharing the visited page's key, summing counts and keeping the
// latest time.
func creditVisits(out map[string]*seen, byKey map[string][]string, vs []history.Visit) {
	for _, v := range vs {
		for _, u := range byKey[merge.Key(v.URL)] {
			if o := out[u]; o != nil {
				o.Visits += v.Visits
				if v.Last.After(o.Last) {
					o.Last = v.Last
				}
				continue
			}
			out[u] = &seen{v.Last, v.Visits}
		}
	}
}

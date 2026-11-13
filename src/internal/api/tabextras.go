package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/tabs"
)

// listTabNotes is every note on tabs, windows and snapshots, newest first.
func (s *Server) listTabNotes(w http.ResponseWriter, r *http.Request) {
	notes, err := s.Data.TabNotes(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"notes": notes})
}

// putTabNote saves a note and tags on a tab (by address), a window or a snapshot; an empty one removes it.
func (s *Server) putTabNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind string   `json:"kind"`
		Key  string   `json:"key"`
		Note string   `json:"note"`
		Tags []string `json:"tags"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Kind != store.NoteTab && in.Kind != store.NoteWindow && in.Kind != store.NoteSnapshot {
		writeErr(w, 400, `kind: "tab", "window" or "snapshot"`)
		return
	}
	in.Key, in.Note = strings.TrimSpace(in.Key), strings.TrimSpace(in.Note)
	if in.Key == "" || len(in.Key) > 4096 || len(in.Note) > 2000 || len(in.Tags) > 20 {
		writeErr(w, 400, "a key up to 4096 characters, a note up to 2000, at most 20 tags")
		return
	}
	tags := []string{}
	for _, t := range in.Tags {
		if t = strings.TrimSpace(t); t != "" && len(t) <= 40 && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	n := store.TabNote{Kind: in.Kind, Key: in.Key, Note: in.Note, Tags: tags, Updated: s.Now().UTC()}
	if err := s.Data.SetTabNote(r.Context(), n); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, _ = s.Bus.Publish("tabs.noted", map[string]any{"kind": n.Kind, "key": n.Key, "removed": n.Note == "" && len(tags) == 0})
	writeJSON(w, 200, map[string]any{"note": n})
}

// deleteSnapshots deletes the workspace pool's snapshots for good (?workspace=, as tabPool): exactly one of ids, before (a day) or everything (which also needs
// confirm: "DELETE ALL"). Owners only: it cannot be undone.
func (s *Server) deleteSnapshots(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs        []string `json:"ids"`
		Before     string   `json:"before"`
		Everything bool     `json:"everything"`
		Confirm    string   `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	given := 0
	for _, b := range []bool{len(in.IDs) > 0, in.Before != "", in.Everything} {
		if b {
			given++
		}
	}
	switch {
	case given != 1:
		writeErr(w, 400, "give exactly one of ids, before, everything")
		return
	case in.Everything && in.Confirm != "DELETE ALL":
		writeErr(w, 400, `deleting every snapshot needs confirm: "DELETE ALL"`)
		return
	case len(in.IDs) > 5000:
		writeErr(w, 400, "at most 5000 ids")
		return
	}
	n, err := s.Tabs.DeleteSnapshots(r.Context(), s.tabPool(r), in.IDs, in.Before, in.Everything)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("tabs.deleted", map[string]any{"deleted": n})
	writeJSON(w, 200, map[string]any{"deleted": n})
}

// purgeTabs forgets private windows in the workspace pool's snapshots (?workspace=, as tabPool): level "pages" keeps their counts, "windows" removes them.
// Owners only.
func (s *Server) purgeTabs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Level          string `json:"level"`
		OlderThanHours int    `json:"older_than_hours"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Level == "" {
		in.Level = "pages"
	}
	if in.OlderThanHours < 0 || in.OlderThanHours > 24*3650 {
		writeErr(w, 400, fmt.Sprintf("older_than_hours: 0 (all) to %d", 24*3650))
		return
	}
	res, err := s.Tabs.PurgePool(r.Context(), s.tabPool(r), in.Level, time.Duration(in.OlderThanHours)*time.Hour)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("tabs.purged", map[string]any{"level": res.Level, "snapshots": res.Snapshots, "windows": res.Windows})
	writeJSON(w, 200, res)
}

// tabsStored says how many snapshots tab history keeps and how many private-window pages they still hold.
func (s *Server) tabsStored(w http.ResponseWriter, r *http.Request) {
	n, pages, err := s.Tabs.Stored(r.Context(), s.tabPool(r))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]int{"snapshots": n, "incognito_pages": pages})
}

// tabPool is the tab-snapshot pool a request reads: that of ?workspace= when given, else of the workspace last
// opened; a workspace that keeps its own snapshots has its own pool, every other one shares one (tabs.PoolFor).
func (s *Server) tabPool(r *http.Request) string {
	p, _ := store.LoadPrefs(r.Context(), s.Data)
	ws := r.URL.Query().Get("workspace")
	if ws == "" {
		ws = p.ActiveWorkspace
	}
	return tabs.PoolFor(p, ws)
}

// openWorkspace records the workspace in use, so background tab snapshots are saved into its pool.
func (s *Server) openWorkspace(w http.ResponseWriter, r *http.Request) {
	x, err := s.Data.Workspace(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	p, err := store.LoadPrefs(r.Context(), s.Data)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if p.ActiveWorkspace != x.ID {
		p.ActiveWorkspace = x.ID
		if err := store.SavePrefs(r.Context(), s.Data, p); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, 200, map[string]any{"active": x.ID, "own_tabs": tabs.PoolFor(p, x.ID) != ""})
}

// setWorkspaceTabs chooses whether a workspace keeps its own tab snapshots ({"own": true}) or uses the shared pool.
// Snapshots already taken stay in the pool they were saved in.
func (s *Server) setWorkspaceTabs(w http.ResponseWriter, r *http.Request) {
	x, err := s.Data.Workspace(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var in struct {
		Own *bool `json:"own"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Own == nil {
		writeErr(w, 400, `give "own": true or false`)
		return
	}
	p, err := store.LoadPrefs(r.Context(), s.Data)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	keep := []string{}
	for _, id := range p.TabsOwn {
		if id != x.ID {
			keep = append(keep, id)
		}
	}
	if *in.Own {
		keep = append(keep, x.ID)
	}
	p.TabsOwn = keep
	if err := store.SavePrefs(r.Context(), s.Data, p); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("preferences.updated", map[string]any{"tabs_own": p.TabsOwn})
	writeJSON(w, 200, map[string]any{"workspace": x.ID, "own_tabs": *in.Own})
}

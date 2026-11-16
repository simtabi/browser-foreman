package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
	"github.com/simtabi/browser-foreman/src/internal/notify"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/tabs"
)

func (s *Server) liveTabs(w http.ResponseWriter, r *http.Request) {
	live, _, err := s.Tabs.Live(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, live)
}

// publishSaved announces a stored snapshot with its counts only: no addresses reach the event stream.
func (s *Server) publishSaved(saved tabs.Saved, reason string) {
	if saved.Stored {
		sum := saved.State.Summary
		_, _ = s.Bus.Publish("tabs.snapshot.saved", map[string]any{"id": saved.ID, "reason": reason, "windows": sum.Windows,
			"tabs": sum.Tabs, "incognito_windows": sum.IncognitoWindows, "incognito_tabs": sum.IncognitoTabs})
	}
}

func (s *Server) takeSnapshot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason string `json:"reason"`
		Force  bool   `json:"force"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Reason == "" {
		in.Reason = "manual"
	}
	if len(in.Reason) > 40 {
		writeErr(w, 400, "reason: at most 40 characters")
		return
	}
	saved, err := s.Tabs.Take(r.Context(), in.Reason, in.Force)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.publishSaved(saved, in.Reason)
	code := 200
	if saved.Stored {
		code = 201
	}
	writeJSON(w, code, saved)
}

// scope reads ?browsers=a,b: nil when absent (all browsers), an empty list when given empty (none).
func scope(r *http.Request) []string {
	if !r.URL.Query().Has("browsers") {
		return nil
	}
	out := []string{}
	for _, n := range strings.Split(r.URL.Query().Get("browsers"), ",") {
		if n = strings.TrimSpace(n); n != "" && len(n) <= 60 && len(out) < 20 {
			out = append(out, n)
		}
	}
	return out
}

// listSnapshots lists snapshots newest first, with their browsers and counts but no pages; those older than
// keep_days are left out unless ?all=1 (they are kept, never deleted). ?browsers= narrows the counts.
func (s *Server) listSnapshots(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 5000 {
			writeErr(w, 400, "limit: 1 to 5000")
			return
		}
		limit = n
	}
	p, err := store.LoadPrefs(r.Context(), s.Data)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rows, err := s.Tabs.Rows(r.Context(), limit, scope(r), s.tabPool(r))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	cutoff := s.Now().Add(-time.Duration(p.KeepDays) * 24 * time.Hour)
	out := []tabs.Row{}
	hidden := 0
	for _, x := range rows {
		if r.URL.Query().Get("all") != "1" && x.At.Before(cutoff) {
			hidden++
			continue
		}
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"snapshots": out, "hidden": hidden})
}

// timeline lists the last ?days (30) of snapshots, or those from ?from= to ?to=, oldest first, with what opened and closed at each.
func (s *Server) timeline(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 3650 {
			writeErr(w, 400, "days: 0 to 3650")
			return
		}
		days = n
	}
	from, to, custom, err := timeRange(r.URL.Query())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var steps []tabs.Step
	if custom {
		steps, err = s.Tabs.TimelineBetween(r.Context(), from, to, scope(r), 40, s.tabPool(r))
	} else {
		steps, err = s.Tabs.Timeline(r.Context(), days, scope(r), 40, s.tabPool(r))
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"days": days, "browsers": scope(r), "snapshots": steps})
}

// ages says when each address was first seen open (POST, since the list can be long).
func (s *Server) ages(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URLs []string `json:"urls"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.URLs) > 5000 {
		writeErr(w, 400, "at most 5000 addresses")
		return
	}
	seen, err := s.Tabs.FirstSeen(r.Context(), in.URLs)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"first_seen": seen})
}

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	st, snap, err := s.Tabs.Snapshot(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no snapshot with that id")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": snap.ID, "at": snap.At, "reason": snap.Reason, "state": st})
}

// restoreSnapshot reopens a snapshot's windows (all, or ?windows by position), each in a new window of its browser.
// Owner only: it opens windows on this computer.
func (s *Server) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Windows []int `json:"windows"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.Tabs.Restore(r.Context(), r.PathValue("id"), in.Windows)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no snapshot with that id")
		return
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("tabs.restored", map[string]any{"id": r.PathValue("id"), "windows": len(res)})
	writeJSON(w, 200, map[string]any{"opened": res})
}

// closeTabs closes tabs in the browsers, after a snapshot that brings them back. Owner only.
func (s *Server) closeTabs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Items []tabs.CloseItem `json:"items"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.Tabs.CloseTabs(r.Context(), in.Items)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("tabs.closed", map[string]any{"closed": out.Closed, "skipped": len(out.Skipped), "undo_snapshot": out.UndoSnapshot})
	writeJSON(w, 200, out)
}

// Background runs the server's own schedules until ctx ends: tab autosave, AI jobs a restart left waiting, and
// re-checking links whose last check
// is older than recheck_days (links never checked wait for a check someone starts).
func (s *Server) Background(ctx context.Context) {
	go s.Tabs.Autosave(ctx, func(saved tabs.Saved) { s.publishSaved(saved, "auto") })
	s.resumeAIJobs(ctx)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
			s.recheckStale(ctx)
		}
	}()
}

// reportSweep waits for a scheduled check and shows one desktop notification when it confirmed links dead or found
// dead ones working again. A check someone started from the page reports there instead.
func (s *Server) reportSweep(ctx context.Context, workspace string, job *linkcheck.Job) {
	job.Wait()
	if s.Notifier == nil || job.View().State != linkcheck.Done {
		return
	}
	s.checks.mu.Lock()
	st := *s.checks.stats[job.ID]
	s.checks.mu.Unlock()
	addr := s.Cfg.Server.Addr
	if s.Listener != nil {
		addr = s.Listener.Addr().String()
	}
	n, ok := notify.Sweep(workspace, st.NewlyConfirmed, st.Fixed, addr)
	if !ok {
		return
	}
	if _, err := s.Notifier.Send(ctx, n); err != nil && s.Console != nil {
		fmt.Fprintf(s.Console, "Desktop notification not shown: %v\n", err) // a courtesy: never fatal
	}
}

// recheckStale starts one check per round, for the first workspace (not archived) holding links due again.
func (s *Server) recheckStale(ctx context.Context) {
	p, err := store.LoadPrefs(ctx, s.Data)
	if err != nil || p.RecheckDays == 0 {
		return
	}
	cutoff := s.Now().Add(-time.Duration(p.RecheckDays) * 24 * time.Hour)
	all, err := s.Data.Workspaces(ctx)
	if err != nil {
		return
	}
	for _, x := range all {
		if x.Archived || x.Head == 0 {
			continue
		}
		t, _, err := s.Data.State(ctx, x.ID, 0)
		if err != nil {
			continue
		}
		known, err := s.Data.Links(ctx, webURLs(t, nil))
		if err != nil {
			continue
		}
		var due []string
		for u, l := range known {
			if l.Checked.Before(cutoff) {
				due = append(due, u)
			}
		}
		if len(due) == 0 {
			continue
		}
		if job, err := s.StartCheck(x.ID, due); err == nil {
			_, _ = s.Bus.Publish("linkcheck.scheduled", map[string]any{"workspace": x.ID, "due": len(due)})
			go s.reportSweep(ctx, x.Name, job)
		}
		return
	}
}

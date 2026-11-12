package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// checkRunner holds the one link check that may run at a time, and the finished ones until the server restarts.
type checkRunner struct {
	mu    sync.Mutex
	jobs  map[string]*linkcheck.Job
	order []string            // job ids, oldest first
	stats map[string]*runStat // per job: what changed
	live  string              // the running job's id, "" when none
}

// runStat counts, for one check, the links that broke and the ones that came back.
type runStat struct {
	Workspace   string `json:"workspace"`
	NewlyBroken int    `json:"newly_broken"`
	Fixed       int    `json:"fixed"`
}

// webURLs lists the http and https addresses of the bookmarks under the given nodes (every bookmark when none).
func webURLs(t store.Tree, nodes []string) []string {
	want := map[string]bool{}
	for _, n := range nodes {
		want[n] = true
	}
	var out []string
	var walk func(n *store.Node, in bool)
	walk = func(n *store.Node, in bool) {
		for _, c := range n.Children {
			hit := in || len(want) == 0 || want[c.ID]
			if c.Kind == store.KindFolder {
				walk(c, hit)
			} else if hit && (strings.HasPrefix(c.URL, "https://") || strings.HasPrefix(c.URL, "http://")) {
				out = append(out, c.URL)
			}
		}
	}
	walk(t.Root, false)
	return out
}

// startCheck checks a workspace's links (or those under `nodes`) in the background. One check runs at a time.
func (s *Server) startCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Workspace string   `json:"workspace"`
		Nodes     []string `json:"nodes"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, err := s.Data.Workspace(r.Context(), in.Workspace)
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
	urls := webURLs(t, in.Nodes)
	if len(urls) == 0 {
		writeErr(w, 400, "no web links to check there")
		return
	}
	job, err := s.StartCheck(x.ID, urls)
	if errors.Is(err, errCheckRunning) {
		writeErr(w, 409, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 202, map[string]any{"job": job.View()})
}

func (s *Server) job(w http.ResponseWriter, r *http.Request) (*linkcheck.Job, bool) {
	s.checks.mu.Lock()
	j := s.checks.jobs[r.PathValue("id")]
	s.checks.mu.Unlock()
	if j == nil {
		writeErr(w, 404, "no link check with that id (finished checks are forgotten when the server restarts)")
		return nil, false
	}
	return j, true
}

// getCheck reports progress; ?results=1 adds every result so far, by URL.
func (s *Server) getCheck(w http.ResponseWriter, r *http.Request) {
	j, ok := s.job(w, r)
	if !ok {
		return
	}
	out := map[string]any{"job": j.View()}
	if r.URL.Query().Get("results") == "1" {
		out["results"] = j.Results()
	}
	writeJSON(w, 200, out)
}

func (s *Server) controlCheck(verb string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		j, ok := s.job(w, r)
		if !ok {
			return
		}
		did := map[string]func() bool{"pause": j.Pause, "resume": j.Resume, "cancel": j.Cancel}[verb]()
		if !did {
			writeErr(w, 409, "the check is "+j.View().State)
			return
		}
		writeJSON(w, 200, map[string]any{"job": j.View()})
	}
}

// getLinks returns what the last checks found for a workspace's links, by URL.
func (s *Server) getLinks(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	links, err := s.Data.Links(r.Context(), webURLs(t, nil))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"links": links})
}

var errCheckRunning = errors.New("a link check is already running")

// StartCheck checks urls (a workspace's) in the background; one check runs at a time (errCheckRunning).
func (s *Server) StartCheck(wsID string, urls []string) (*linkcheck.Job, error) {
	s.checks.mu.Lock()
	defer s.checks.mu.Unlock()
	if s.checks.jobs == nil {
		s.checks.jobs, s.checks.stats = map[string]*linkcheck.Job{}, map[string]*runStat{}
	}
	if s.checks.live != "" {
		return nil, fmt.Errorf("%w: %s", errCheckRunning, s.checks.live)
	}
	prev, err := s.Data.Links(context.Background(), urls)
	if err != nil {
		return nil, err
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := "lc_" + hex.EncodeToString(b)
	var mu sync.Mutex
	var batch []store.LinkResult
	changed, dead := 0, 0
	stat := &runStat{Workspace: wsID}
	flush := func() {
		mu.Lock()
		todo := batch
		batch = nil
		mu.Unlock()
		if len(todo) > 0 {
			_ = s.Data.SaveLinks(context.Background(), todo)
		}
	}
	job := s.Checker.Start(id, urls, 12, func(res linkcheck.Result) {
		before, known := prev[res.URL]
		mu.Lock()
		if known && res.Status == linkcheck.StatusDead && before.Status != linkcheck.StatusDead {
			stat.NewlyBroken++
		}
		if known && before.Status == linkcheck.StatusDead && res.Status == linkcheck.StatusOK {
			stat.Fixed++
		}
		mu.Unlock()
		// A check that could not reach the site says nothing new about it: keep what an earlier check found, so a
		// network hiccup never turns a working or dead link into "could not verify".
		if res.Status == linkcheck.StatusUnverified && known && before.Status != linkcheck.StatusUnverified {
			return
		}
		mu.Lock()
		batch = append(batch, res)
		full := len(batch) >= 50
		if res.Status == linkcheck.StatusDead {
			dead++
		}
		if !known || before.Status != res.Status {
			changed++
		}
		mu.Unlock()
		if known && before.Status != res.Status {
			_, _ = s.Bus.Publish("link.checked", map[string]any{"url": res.URL, "status": res.Status, "previous": before.Status, "final": res.Final, "note": res.Note})
		}
		if full {
			flush()
		}
	})
	s.checks.jobs[id], s.checks.live = job, id
	s.checks.stats[id] = stat
	s.checks.order = append(s.checks.order, id)
	_, _ = s.Bus.Publish("linkcheck.started", map[string]any{"id": id, "workspace": wsID, "total": job.Total})
	go func() {
		job.Wait()
		flush()
		v := job.View()
		s.checks.mu.Lock()
		s.checks.live = ""
		s.checks.mu.Unlock()
		mu.Lock()
		_, _ = s.Bus.Publish("linkcheck.finished", map[string]any{"id": id, "workspace": wsID, "state": v.State, "checked": v.Checked, "total": v.Total, "changed": changed, "dead": dead})
		mu.Unlock()
	}()
	return job, nil
}

// listChecks reports the running check and the earlier ones since the server started, newest first.
func (s *Server) listChecks(w http.ResponseWriter, _ *http.Request) {
	s.checks.mu.Lock()
	defer s.checks.mu.Unlock()
	type run struct {
		linkcheck.View
		runStat
	}
	out := []run{}
	for i := len(s.checks.order) - 1; i >= 0; i-- {
		id := s.checks.order[i]
		out = append(out, run{s.checks.jobs[id].View(), *s.checks.stats[id]})
	}
	var live any
	if s.checks.live != "" {
		live = s.checks.live
	}
	writeJSON(w, 200, map[string]any{"running": live, "runs": out})
}

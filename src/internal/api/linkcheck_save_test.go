package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// failingLinks is a store whose link results cannot be saved (a full disk, a locked database).
type failingLinks struct{ store.Store }

func (failingLinks) SaveLinks(context.Context, []store.LinkResult) error {
	return errors.New("disk full")
}

// Results that could not be saved are counted and reported, not dropped while the check says it finished cleanly.
func TestLinkResultsThatCannotBeSavedAreReported(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer site.Close()
	s := newServer(t, "tok")
	linkWorkspace(t, s, site.URL)
	s.Data = failingLinks{s.Data}
	var console strings.Builder
	s.Console = &console
	job, err := s.StartCheck("links", []string{site.URL + "/a", site.URL + "/b"})
	if err != nil {
		t.Fatal(err)
	}
	job.Wait()
	var finished map[string]any
	for range 100 {
		evs, _ := s.Bus.Since(0)
		for _, e := range evs {
			if e.Type == "linkcheck.finished" {
				_ = json.Unmarshal(e.Data, &finished)
			}
		}
		if finished != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if finished == nil || finished["save_errors"] != float64(2) {
		t.Fatalf("finished %+v", finished)
	}
	s.checks.mu.Lock()
	st := s.checks.stats[job.ID].snapshot()
	s.checks.mu.Unlock()
	if st.SaveErrors != 2 || !strings.Contains(console.String(), "not saved") {
		t.Fatalf("stat %+v, console %q", st, console.String())
	}
}

// Only the newest checks are remembered; the server does not keep every check since it started.
func TestOldChecksAreForgotten(t *testing.T) {
	s := newServer(t, "tok")
	s.checks.jobs, s.checks.stats = map[string]*linkcheck.Job{}, map[string]*statBox{}
	for i := range keptChecks + 7 {
		id := "lc_" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
		s.checks.jobs[id], s.checks.stats[id] = &linkcheck.Job{ID: id}, &statBox{}
		s.checks.order = append(s.checks.order, id)
		s.forgetOldChecks()
	}
	if len(s.checks.order) != keptChecks || len(s.checks.jobs) != keptChecks || len(s.checks.stats) != keptChecks {
		t.Fatalf("kept %d, %d jobs, %d stats", len(s.checks.order), len(s.checks.jobs), len(s.checks.stats))
	}
}

// Snapshot jobs are forgotten the same way.
func TestOldSnapshotJobsAreForgotten(t *testing.T) {
	s := newServer(t, "tok")
	s.snaps.jobs = map[string]*snapJob{}
	t0 := time.Now()
	for i := range keptChecks + 5 {
		id := "sn_" + strings.Repeat("x", i+1)
		s.snaps.jobs[id] = &snapJob{ID: id, Started: t0.Add(time.Duration(i) * time.Second), State: "done"}
		s.forgetOldSnapJobs()
	}
	if len(s.snaps.jobs) != keptChecks || s.snaps.jobs["sn_x"] != nil {
		t.Fatalf("kept %d, the oldest still there: %v", len(s.snaps.jobs), s.snaps.jobs["sn_x"] != nil)
	}
}

// A check's results say, for each link it changed, what that link was before: a report can then tell a link that
// just broke from one that was dead already.
func TestResultsSayWhatEachChangedLinkWasBefore(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer site.Close()
	s := newServer(t, "tok")
	linkWorkspace(t, s, site.URL)
	old := time.Now().Add(-48 * time.Hour)
	_ = s.Data.SaveLinks(context.Background(), []store.LinkResult{
		{URL: site.URL + "/a", Status: "ok", Checked: old},
		{URL: site.URL + "/b", Status: "dead", Checked: old, DeadSince: old},
	})
	job, err := s.StartCheck("links", []string{site.URL + "/a", site.URL + "/b"})
	if err != nil {
		t.Fatal(err)
	}
	job.Wait()
	waitFinished(t, s) // the last results are saved after Wait returns, then linkcheck.finished is published
	var out struct{ Previous map[string]string }
	_ = json.Unmarshal(send(s.Handler(), "GET", "/api/v1/linkchecks/"+job.ID+"?results=1", "").Body.Bytes(), &out)
	if out.Previous[site.URL+"/a"] != "ok" {
		t.Fatalf("previous %v", out.Previous)
	}
	if _, ok := out.Previous[site.URL+"/b"]; ok {
		t.Fatalf("a link dead before and after is listed as changed: %v", out.Previous)
	}
}

// waitFinished waits for linkcheck.finished, which the server publishes once a check's last results are saved.
func waitFinished(t *testing.T, s *Server) {
	t.Helper()
	for range 200 {
		evs, _ := s.Bus.Since(0)
		for _, e := range evs {
			if e.Type == "linkcheck.finished" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no linkcheck.finished")
}

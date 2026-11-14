package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func linkWorkspace(t *testing.T, s *Server, base string) {
	t.Helper()
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "links", "Links", time.Now())
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "bar", Kind: store.KindFolder, Title: "Bookmarks Bar", Children: []*store.Node{
			{ID: "a", Kind: store.KindBookmark, Title: "A", URL: base + "/a"},
			{ID: "dev", Kind: store.KindFolder, Title: "Dev", Children: []*store.Node{
				{ID: "b", Kind: store.KindBookmark, Title: "B", URL: base + "/b"},
				{ID: "f", Kind: store.KindBookmark, Title: "FTP", URL: "ftp://x/"}}}}}}}}
	if _, err := s.Data.Commit(ctx, "links", 0, store.Change{Op: "import", Summary: "x", Tree: tr}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func waitCheck(t *testing.T, h http.Handler, id string) map[string]store.LinkResult {
	t.Helper()
	for range 200 {
		var out struct {
			Job     struct{ Done bool }
			Results map[string]store.LinkResult
		}
		_ = json.Unmarshal(send(h, "GET", "/api/v1/linkchecks/"+id+"?results=1", "").Body.Bytes(), &out)
		if out.Job.Done {
			return out.Results
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the check never finished")
	return nil
}

func start(t *testing.T, h http.Handler, body string) string {
	t.Helper()
	w := send(h, "POST", "/api/v1/linkchecks", body)
	var out struct{ Job struct{ ID string } }
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 202 || out.Job.ID == "" {
		t.Fatalf("start %d %s", w.Code, w.Body)
	}
	return out.Job.ID
}

func TestLinkChecksStoreResultsAndReportChanges(t *testing.T) {
	var bGone, throttled atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if throttled.Load() {
			http.Error(w, "slow down", 429)
			return
		}
		if r.URL.Path == "/b" && bGone.Load() {
			http.Error(w, "gone", 404)
			return
		}
		w.Write([]byte("hello"))
	}))
	defer site.Close()
	s := newServer(t, "tok")
	linkWorkspace(t, s, site.URL)
	h := s.Handler()

	res := waitCheck(t, h, start(t, h, `{"workspace":"links"}`))
	if len(res) != 2 || res[site.URL+"/a"].Status != "ok" || res[site.URL+"/b"].Status != "ok" {
		t.Fatalf("the ftp link must be left out; got %+v", res)
	}
	var stored struct{ Links map[string]store.LinkResult }
	_ = json.Unmarshal(send(h, "GET", "/api/v1/workspaces/links/links", "").Body.Bytes(), &stored)
	if len(stored.Links) != 2 {
		t.Fatalf("stored %+v", stored.Links)
	}
	// Only the folder Dev, after B went away: one changed status, one event.
	bGone.Store(true)
	res = waitCheck(t, h, start(t, h, `{"workspace":"links","nodes":["dev"]}`))
	if len(res) != 1 || res[site.URL+"/b"].Status != "dead" {
		t.Fatalf("%+v", res)
	}
	time.Sleep(50 * time.Millisecond)
	evs, _ := s.Bus.Since(0)
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Type)
	}
	if got := strings.Join(kinds, ","); got != "linkcheck.started,linkcheck.finished,linkcheck.started,link.checked,linkcheck.finished" {
		t.Fatal(got)
	}
	_ = json.Unmarshal(send(h, "GET", "/api/v1/workspaces/links/links", "").Body.Bytes(), &stored)
	if stored.Links[site.URL+"/b"].Status != "dead" || stored.Links[site.URL+"/a"].Status != "ok" {
		t.Fatalf("%+v", stored.Links)
	}
	// The site asks us to slow down: "could not verify" must not replace what was known.
	throttled.Store(true)
	waitCheck(t, h, start(t, h, `{"workspace":"links"}`))
	_ = json.Unmarshal(send(h, "GET", "/api/v1/workspaces/links/links", "").Body.Bytes(), &stored)
	if stored.Links[site.URL+"/a"].Status != "ok" {
		t.Fatalf("an unreachable check overwrote a known result: %+v", stored.Links[site.URL+"/a"])
	}
}

func TestOneCheckAtATimeAndControls(t *testing.T) {
	gate := make(chan struct{})
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
		case <-r.Context().Done():
		}
	}))
	defer site.Close()
	s := newServer(t, "tok")
	linkWorkspace(t, s, site.URL)
	h := s.Handler()
	id := start(t, h, `{"workspace":"links"}`)
	if w := send(h, "POST", "/api/v1/linkchecks", `{"workspace":"links"}`); w.Code != 409 {
		t.Fatalf("second check %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/linkchecks/"+id+"/pause", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"paused"`) {
		t.Fatalf("pause %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/linkchecks/"+id+"/resume", ""); w.Code != 200 {
		t.Fatalf("resume %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/linkchecks/"+id+"/cancel", ""); w.Code != 200 {
		t.Fatalf("cancel %d", w.Code)
	}
	waitCheck(t, h, id)
	if w := send(h, "POST", "/api/v1/linkchecks/"+id+"/cancel", ""); w.Code != 409 {
		t.Fatalf("cancel twice %d", w.Code)
	}
	if w := send(h, "GET", "/api/v1/linkchecks/lc_nope", ""); w.Code != 404 {
		t.Fatalf("missing %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/linkchecks", `{"workspace":"nope"}`); w.Code != 404 {
		t.Fatalf("missing workspace %d", w.Code)
	}
	close(gate)
}

func TestADeadLinkIsConfirmedADayLaterAndGetsItsArchiveCopy(t *testing.T) {
	var asked atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wayback":
			asked.Add(1)
			w.Write([]byte(`{"archived_snapshots":{"closest":{"available":true,"status":"200","url":"http://web.archive.org/web/2020/x"}}}`))
		case "/b":
			http.Error(w, "gone", 404)
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Write([]byte(`<html><head><title>A</title><meta property="og:description" content="About A"></head><body>Plenty of words here.</body></html>`))
		}
	}))
	defer site.Close()
	s := newServer(t, "tok")
	s.WaybackAPI = site.URL + "/wayback"
	linkWorkspace(t, s, site.URL)
	h := s.Handler()
	now := time.Now()
	s.Checker.Now = func() time.Time { return now }
	if w := send(h, "PUT", "/api/v1/preferences", `{"wayback":true}`); w.Code != 200 {
		t.Fatalf("prefs %d %s", w.Code, w.Body)
	}
	stored := func() map[string]store.LinkResult {
		var out struct{ Links map[string]store.LinkResult }
		_ = json.Unmarshal(send(h, "GET", "/api/v1/workspaces/links/links", "").Body.Bytes(), &out)
		return out.Links
	}
	res := waitCheck(t, h, start(t, h, `{"workspace":"links"}`))
	if b := res[site.URL+"/b"]; b.Status != "dead" || b.Confirmed || b.DeadSince.IsZero() {
		t.Fatalf("first sighting, as the run reports it: %+v", b)
	}
	a := stored()[site.URL+"/a"]
	if a.Frame != "refused" || a.Meta == nil || a.Meta.Description != "About A" {
		t.Fatalf("framing and description not kept: %+v", a)
	}
	if asked.Load() != 0 {
		t.Fatal("the archive was asked about a link not yet confirmed dead")
	}
	now = now.Add(25 * time.Hour)
	res = waitCheck(t, h, start(t, h, `{"workspace":"links"}`))
	if b := res[site.URL+"/b"]; !b.Confirmed {
		t.Fatalf("a day later: %+v", b)
	}
	for range 100 {
		if stored()[site.URL+"/b"].Archive != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b := stored()[site.URL+"/b"]; !b.Confirmed || b.Archive != "https://web.archive.org/web/2020/x" || asked.Load() != 1 {
		t.Fatalf("stored %+v, archive asked %d times", b, asked.Load())
	}
	// Off again: the next confirmed-dead link is not sent anywhere.
	send(h, "PUT", "/api/v1/preferences", `{"wayback":false}`)
	s.lookUpArchives([]store.LinkResult{{URL: site.URL + "/other", Status: "dead", Confirmed: true}})
	if asked.Load() != 1 {
		t.Fatal("the archive was asked with the lookup turned off")
	}
}

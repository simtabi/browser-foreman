package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
	"github.com/simtabi/browser-foreman/src/internal/media"
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

func TestFailingGitHubLinksAreSettledThroughTheAPI(t *testing.T) {
	s := newServer(t, "tok")
	ctx := context.Background()
	t0 := time.Now().Add(-48 * time.Hour)
	s.Checker.Now = func() time.Time { return t0.Add(48 * time.Hour) }
	var asked []string
	s.GitHub = func(_ context.Context, path string) linkcheck.GitHubAnswer {
		asked = append(asked, path)
		switch path {
		case "repos/alive/private-looking":
			return linkcheck.GitHubAnswer{Code: 200, Body: []byte(`{"full_name":"alive/private-looking"}`)}
		case "repos/alive/old":
			return linkcheck.GitHubAnswer{Code: 200, Body: []byte(`{"full_name":"alive/new"}`)}
		case "repos/spent/x":
			return linkcheck.GitHubAnswer{Code: 403, Limited: true}
		case "users/alive":
			return linkcheck.GitHubAnswer{Code: 200}
		}
		return linkcheck.GitHubAnswer{Code: 404}
	}
	dead := func(u string) store.LinkResult {
		return store.LinkResult{URL: u, Status: "dead", Kind: "page", Checked: t0, DeadSince: t0}
	}
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{dead("https://github.com/alive/private-looking"), dead("https://github.com/alive/old"),
		dead("https://github.com/alive/gone"), dead("https://github.com/spent/x"), dead("https://github.com/never/asked")})
	s.askGitHub([]string{"https://example.com/not-github", "https://github.com/alive/private-looking", "https://github.com/alive/old",
		"https://github.com/alive/gone", "https://github.com/spent/x", "https://github.com/never/asked"})
	got, _ := s.Data.Links(ctx, nil)
	if r := got["https://github.com/alive/private-looking"]; r.Status != "ok" || !r.DeadSince.IsZero() {
		t.Errorf("cleared: %+v", r)
	}
	if r := got["https://github.com/alive/old"]; r.Status != "moved" || r.Final != "https://github.com/alive/new" {
		t.Errorf("renamed: %+v", r)
	}
	if r := got["https://github.com/alive/gone"]; r.Status != "dead" || !r.Confirmed || !strings.Contains(r.Note, "owner still exists") {
		t.Errorf("still dead, a day later, confirmed: %+v", r)
	}
	if r := got["https://github.com/never/asked"]; r.Note != "" || strings.Contains(strings.Join(asked, ","), "never") {
		t.Errorf("asked after the budget was spent: %+v %v", r, asked)
	}
	// The archive is not asked about a link GitHub cleared.
	var archived []string
	s.WaybackAPI = "http://127.0.0.1:1/unused"
	_ = s.Data.SavePrefsDoc(ctx, []byte(`{"wayback":true,"open_with":["System default"]}`))
	s.Checker.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		archived = append(archived, r.URL.Query().Get("url"))
		return nil, context.Canceled
	})
	s.lookUpArchives([]store.LinkResult{{URL: "https://github.com/alive/private-looking", Status: "dead", Confirmed: true},
		{URL: "https://github.com/alive/gone", Status: "dead", Confirmed: true}})
	if strings.Join(archived, ",") != "https://github.com/alive/gone" {
		t.Fatalf("archive asked about %v", archived)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A run asks GitHub's API about the GitHub links it found failing, once it finishes.
func TestARunAsksGitHubAboutItsFailingGitHubLinks(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "links", "Links", time.Now())
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "a", Kind: store.KindBookmark, Title: "Repo", URL: "https://github.com/alive/repo"}}}}
	_, _ = s.Data.Commit(ctx, "links", 0, store.Change{Op: "import", Summary: "x", Tree: tr}, time.Now())
	// Every web request answers 404, as github.com does for a private repository to anyone not signed in.
	s.Checker.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})
	s.GitHub = func(_ context.Context, path string) linkcheck.GitHubAnswer {
		if path == "repos/alive/repo" {
			return linkcheck.GitHubAnswer{Code: 200, Body: []byte(`{"full_name":"alive/repo"}`)}
		}
		return linkcheck.GitHubAnswer{Code: 404}
	}
	waitCheck(t, h, start(t, h, `{"workspace":"links"}`))
	for range 100 {
		if got, _ := s.Data.Links(ctx, nil); got["https://github.com/alive/repo"].Status == "ok" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := s.Data.Links(ctx, nil)
	t.Fatalf("%+v", got["https://github.com/alive/repo"])
}

// When gh cannot answer at all (signed out, no network), the run's own results stay and nothing more is asked.
func TestGitHubThatCannotAnswerChangesNothing(t *testing.T) {
	s := newServer(t, "tok")
	ctx := context.Background()
	asked := 0
	s.GitHub = func(context.Context, string) linkcheck.GitHubAnswer { asked++; return linkcheck.GitHubAnswer{} }
	r := store.LinkResult{URL: "https://github.com/a/b", Status: "unverified", Note: "timed out", Checked: time.Now()}
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{r, {URL: "https://github.com/c/d", Status: "dead", Checked: time.Now()}})
	s.askGitHub([]string{"https://github.com/a/b", "https://github.com/c/d"})
	got, _ := s.Data.Links(ctx, nil)
	if got["https://github.com/a/b"].Note != "timed out" || got["https://github.com/c/d"].Status != "dead" || asked != 1 {
		t.Fatalf("%+v, asked %d", got, asked)
	}
}

// Links a plain request could not settle are opened in Chrome and judged from what it shows.
func TestBlockedLinksAreSettledInChrome(t *testing.T) {
	s := newServer(t, "tok")
	ctx := context.Background()
	words := strings.Repeat("Plenty of real words on this page. ", 10)
	var opened []string
	s.Snapper = &media.Snapper{Chrome: "/x/chrome",
		Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil },
		Dump: func(_ context.Context, argv []string) ([]byte, error) {
			u := argv[len(argv)-1]
			opened = append(opened, u)
			if strings.Contains(u, "gone") {
				return []byte("<html><title>Page not found</title></html>"), nil
			}
			return []byte("<html><head><title>Docs</title></head><body>" + words + "</body></html>"), nil
		}}
	now := time.Now()
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{
		{URL: "https://blocked.example/", Status: "unverified", Code: 403, Note: "HTTP 403", Checked: now},
		{URL: "https://blocked.example/gone", Status: "unverified", Code: 403, Note: "HTTP 403", Checked: now},
		{URL: "https://dead.example/", Status: "dead", Checked: now}})
	s.renderBlocked([]string{"https://blocked.example/", "https://blocked.example/gone", "https://dead.example/"})
	got, _ := s.Data.Links(ctx, nil)
	if r := got["https://blocked.example/"]; r.Status != "ok" || r.Note != "loaded normally in Chrome" {
		t.Errorf("%+v", r)
	}
	if r := got["https://blocked.example/gone"]; r.Status != "dead" || r.Confirmed || r.DeadSince.IsZero() {
		t.Errorf("found dead once in Chrome: dead, not yet confirmed %+v", r)
	}
	if slices.Contains(opened, "https://dead.example/") {
		t.Error("a link already settled was opened")
	}
	// Turned off: nothing is opened.
	opened = nil
	_ = s.Data.SavePrefsDoc(ctx, []byte(`{"render_checks":false,"open_with":["System default"]}`))
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{{URL: "https://blocked.example/again", Status: "unverified", Checked: now}})
	s.renderBlocked([]string{"https://blocked.example/again"})
	if len(opened) != 0 {
		t.Fatalf("opened with render checks off: %v", opened)
	}
}

// A run opens in Chrome the links it could not settle, once it finishes.
func TestARunSettlesBlockedLinksInChrome(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "links", "Links", time.Now())
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "a", Kind: store.KindBookmark, Title: "Shop", URL: "https://shop.example/item"}}}}
	_, _ = s.Data.Commit(ctx, "links", 0, store.Change{Op: "import", Summary: "x", Tree: tr}, time.Now())
	// Scripts get 403, as bot protection answers them; Chrome sees the real page.
	s.Checker.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})
	words := strings.Repeat("Plenty of real words on this page. ", 10)
	s.Snapper = &media.Snapper{Chrome: "/x/chrome",
		Resolve: func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil },
		Dump: func(context.Context, []string) ([]byte, error) {
			return []byte("<html><head><title>Item</title></head><body>" + words + "</body></html>"), nil
		}}
	waitCheck(t, h, start(t, h, `{"workspace":"links"}`))
	for range 100 {
		if got, _ := s.Data.Links(ctx, nil); got["https://shop.example/item"].Status == "ok" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := s.Data.Links(ctx, nil)
	t.Fatalf("%+v", got["https://shop.example/item"])
}

// Failing Stack Overflow questions are settled through the Stack Exchange API, a hundred to a request.
func TestFailingStackOverflowLinksAreSettledThroughTheAPI(t *testing.T) {
	s := newServer(t, "tok")
	ctx := context.Background()
	s.Checker.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		body := `{"items":[]}`
		if strings.Contains(r.URL.Path, "/questions/") && strings.Contains(r.URL.Path, "11") {
			body = `{"items":[{"question_id":11}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})
	now := time.Now()
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{
		{URL: "https://stackoverflow.com/questions/11/x", Status: "unverified", Code: 403, Note: "HTTP 403", Checked: now},
		{URL: "https://stackoverflow.com/questions/12/y", Status: "unverified", Code: 403, Note: "HTTP 403", Checked: now}})
	s.askStackExchange([]string{"https://stackoverflow.com/questions/11/x", "https://stackoverflow.com/questions/12/y"})
	got, _ := s.Data.Links(ctx, nil)
	if r := got["https://stackoverflow.com/questions/11/x"]; r.Status != "ok" {
		t.Errorf("%+v", r)
	}
	if r := got["https://stackoverflow.com/questions/12/y"]; r.Status != "dead" || r.DeadSince.IsZero() {
		t.Errorf("%+v", r)
	}
}

// A run asks the Stack Exchange API about the Stack Overflow links it could not settle, once it finishes.
func TestARunAsksStackExchange(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "links", "Links", time.Now())
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "a", Kind: store.KindBookmark, Title: "Q", URL: "https://stackoverflow.com/questions/11/x"}}}}
	_, _ = s.Data.Commit(ctx, "links", 0, store.Change{Op: "import", Summary: "x", Tree: tr}, time.Now())
	s.StackExchangeAPI = "https://api.example/2.3"
	s.Checker.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.example" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"items":[{"question_id":11}]}`)), Header: http.Header{}, Request: r}, nil
		}
		return &http.Response{StatusCode: 403, Body: http.NoBody, Header: http.Header{}, Request: r}, nil // scripts get 403
	})
	_ = s.Data.SavePrefsDoc(ctx, []byte(`{"render_checks":false,"open_with":["System default"]}`))
	waitCheck(t, h, start(t, h, `{"workspace":"links"}`))
	for range 100 {
		if got, _ := s.Data.Links(ctx, nil); got["https://stackoverflow.com/questions/11/x"].Status == "ok" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := s.Data.Links(ctx, nil)
	t.Fatalf("%+v", got["https://stackoverflow.com/questions/11/x"])
}

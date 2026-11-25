package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/ai"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// aiWorkspace holds two untagged bookmarks and one tagged, and two look-alike pairs: one sharing an address (certain)
// and one only sharing a title (for a model to judge).
func aiWorkspace(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "w", "W", time.Now())
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "bar", Kind: store.KindFolder, Title: "Bookmarks Bar", Children: []*store.Node{
			{ID: "dev", Kind: store.KindFolder, Title: "Dev", Children: []*store.Node{
				{ID: "b1", Kind: store.KindBookmark, Title: "Go docs", URL: "https://go.dev/doc/"},
				{ID: "b2", Kind: store.KindBookmark, Title: "Rust book", URL: "https://doc.rust-lang.org/book/", Tags: []string{"rust"}},
				{ID: "b3", Kind: store.KindBookmark, Title: "Zig guide", URL: "https://ziglang.org/learn/"},
			}},
			{ID: "s1", Kind: store.KindBookmark, Title: "Release notes for version two", URL: "https://news.example/notes?v=1"},
			{ID: "s2", Kind: store.KindBookmark, Title: "Release notes for version two", URL: "https://news.example/notes?v=2"},
			{ID: "a1", Kind: store.KindBookmark, Title: "Home", URL: "https://same.example/"},
			{ID: "a2", Kind: store.KindBookmark, Title: "Home again", URL: "https://www.same.example"},
		}},
	}}}
	if _, err := s.Data.Commit(ctx, "w", 0, store.Change{Op: "import", Summary: "x", Tree: tr}, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func node(t *testing.T, s *Server, id string) *store.Node {
	t.Helper()
	tr, _, err := s.Data.State(context.Background(), "w", 0)
	if err != nil {
		t.Fatal(err)
	}
	var hit *store.Node
	visitBookmarks(tr.Root, "", func(n *store.Node, _ string) {
		if n.ID == id {
			hit = n
		}
	})
	return hit
}

func TestAnAITagJobWaitsForAClientAndApplies(t *testing.T) {
	s := newServer(t, apiTok)
	aiWorkspace(t, s)
	h := s.Handler()
	w := send(h, "POST", "/api/v1/ai/jobs", `{"kind":"tag","workspace":"w","folder":"Bookmarks Bar/Dev","apply":true}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"size":2`) || !strings.Contains(w.Body.String(), "MCP client") {
		t.Fatalf("%d %s", w.Code, w.Body) // b2 has tags already: two to tag
	}
	w = send(h, "POST", "/api/v1/ai/jobs/next", `{"max":5,"by":"claude-code"}`)
	var next struct {
		Jobs []ai.Job
	}
	_ = json.Unmarshal(w.Body.Bytes(), &next)
	if w.Code != 200 || len(next.Jobs) != 1 || len(next.Jobs[0].Items) != 2 || next.Jobs[0].State != ai.Working {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/ai/jobs/next", `{}`); !strings.Contains(w.Body.String(), `"jobs":[]`) {
		t.Fatalf("a claimed job was handed out twice: %s", w.Body)
	}
	id := next.Jobs[0].ID
	w = send(h, "POST", "/api/v1/ai/jobs/"+id+"/results", `{"results":{"tags":[{"id":"b1","tags":["go","docs"]},{"id":"s1","tags":["outside"]}]},"by":"claude-code"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"done"`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if n := node(t, s, "b1"); strings.Join(n.Tags, ",") != "docs,go" {
		t.Fatalf("b1 %+v", n)
	}
	if n := node(t, s, "s1"); len(n.Tags) != 0 {
		t.Fatalf("an answer tagged a bookmark outside the job: %+v", n)
	}
	if w := send(h, "POST", "/api/v1/ai/jobs/"+id+"/results", `{"results":{"tags":[{"id":"b1","tags":["x"]}]}}`); w.Code != 400 {
		t.Fatalf("a job was answered twice: %d %s", w.Code, w.Body)
	}
}

// fakeProvider answers every dedupe group with all members the same page.
type fakeProvider struct{ calls atomic.Int32 } // Answer runs on the job goroutine, read by the test

func (*fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Answer(_ context.Context, j ai.Job) (ai.Results, error) {
	f.calls.Add(1)
	var r ai.Results
	for _, g := range j.Groups {
		v := ai.Verdict{Keep: g.Keep, Reason: "same release notes"}
		for _, m := range g.Members {
			v.Same = append(v.Same, m.ID)
		}
		r.Verdicts = append(r.Verdicts, v)
	}
	return r, nil
}

func TestAProviderJudgesOnlyUncertainDuplicates(t *testing.T) {
	s := newServer(t, apiTok)
	aiWorkspace(t, s)
	f := &fakeProvider{}
	s.AI = f
	h := s.Handler()
	w := send(h, "POST", "/api/v1/ai/jobs", `{"kind":"dedupe","workspace":"w","apply":true}`)
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"answered_by":"fake"`) || !strings.Contains(w.Body.String(), `"size":1`) {
		t.Fatalf("%d %s", w.Code, w.Body) // one group: s1/s2; a1/a2 share an address and need no model
	}
	var done bool
	for range 100 {
		if w := send(h, "GET", "/api/v1/ai/jobs", ""); strings.Contains(w.Body.String(), `"state":"done"`) {
			done = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !done || f.calls.Load() != 1 {
		t.Fatalf("done %v, calls %d", done, f.calls.Load())
	}
	tr, _, _ := s.Data.State(context.Background(), "w", 0)
	if len(tr.Trash) != 1 || !strings.Contains(tr.Trash[0].Reason, "judged the same page by a model") {
		t.Fatalf("trash %+v", tr.Trash)
	}
}

func TestAIJobsRefuseWhatTheyCannotDo(t *testing.T) {
	s := newServer(t, apiTok)
	aiWorkspace(t, s)
	h := s.Handler()
	for body, want := range map[string]string{
		`{"kind":"summarise","workspace":"w"}`:              "kind is tag or dedupe",
		`{"kind":"tag","workspace":"w","folder":"Nowhere"}`: "no bookmarks",
		`{"kind":"tag","workspace":"w","limit":5000}`:       "at most 1000",
		`{"kind":"tag","workspace":"missing"}`:              "",
	} {
		w := send(h, "POST", "/api/v1/ai/jobs", body)
		if w.Code < 400 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
}

func TestAIJobsOutliveARestart(t *testing.T) {
	s := newServer(t, apiTok)
	aiWorkspace(t, s)
	if w := send(s.Handler(), "POST", "/api/v1/ai/jobs", `{"kind":"tag","workspace":"w"}`); w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if st, err := os.Stat(filepath.Join(s.Cfg.Storage.Path, "ai-jobs.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the queue file: %v %v", st, err)
	}
	// A new server on the same state folder, with a provider: it takes the waiting job and answers it.
	again := newServer(t, apiTok)
	again.Cfg.Storage.Path, again.Data = s.Cfg.Storage.Path, s.Data
	f := &tagger{}
	again.AI = f
	again.resumeAIJobs(context.Background())
	for range 100 {
		if w := send(again.Handler(), "GET", "/api/v1/ai/jobs", ""); strings.Contains(w.Body.String(), `"state":"done"`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the job did not survive the restart: calls %d", f.calls.Load())
}

// tagger tags every bookmark of a job "x".
type tagger struct{ calls atomic.Int32 } // Answer runs on the job goroutine

func (*tagger) Name() string { return "tagger" }
func (f *tagger) Answer(_ context.Context, j ai.Job) (ai.Results, error) {
	f.calls.Add(1)
	var r ai.Results
	for _, it := range j.Items {
		r.Tags = append(r.Tags, ai.TagResult{ID: it.ID, Tags: []string{"x"}})
	}
	return r, nil
}

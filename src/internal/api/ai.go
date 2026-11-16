package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/ai"
	"github.com/simtabi/browser-foreman/src/internal/dupes"
	"github.com/simtabi/browser-foreman/src/internal/edit"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// aiState is the queue of AI jobs. It lives in the server: a job is short work, and one lost to a restart is made
// again in a moment.
type aiState struct {
	mu    sync.Mutex
	jobs  map[string]*ai.Job
	order []string
}

// jobLease is how long a claimed job stays with whoever claimed it before another may take it.
const jobLease = 15 * time.Minute

func (q *aiState) add(j *ai.Job) {
	if q.jobs == nil {
		q.jobs = map[string]*ai.Job{}
	}
	q.jobs[j.ID] = j
	q.order = append(q.order, j.ID)
	if len(q.order) > 500 { // keep the newest 500
		delete(q.jobs, q.order[0])
		q.order = q.order[1:]
	}
}

type aiJobIn struct {
	Kind      string `json:"kind"`
	Workspace string `json:"workspace"`
	Folder    string `json:"folder"`
	Apply     bool   `json:"apply"`
	Limit     int    `json:"limit"` // tag jobs: bookmarks at most (200 when 0, up to 1000)
	Retag     bool   `json:"retag"` // tag jobs: also bookmarks that already have tags
}

// createAIJobs makes jobs from a workspace and, with a provider configured, starts answering them. Owner only: it
// decides what leaves this computer.
func (s *Server) createAIJobs(w http.ResponseWriter, r *http.Request) {
	var in aiJobIn
	if !decode(w, r, &in) {
		return
	}
	jobs, err := s.buildJobs(r.Context(), in)
	if err != nil {
		code := 400
		if errors.Is(err, store.ErrNotFound) {
			code = 404
		}
		writeErr(w, code, err.Error())
		return
	}
	s.ai.mu.Lock()
	for _, j := range jobs {
		s.ai.add(j)
	}
	s.ai.mu.Unlock()
	answeredBy := "an MCP client: it calls next_ai_jobs, or use the process-queue prompt"
	if s.AI != nil {
		answeredBy = s.AI.Name()
		go s.runJobs(context.WithoutCancel(r.Context()), jobs)
	}
	_, _ = s.Bus.Publish("ai.jobs.created", map[string]any{"workspace": in.Workspace, "kind": in.Kind, "jobs": len(jobs)})
	writeJSON(w, 201, map[string]any{"jobs": summaries(jobs), "answered_by": answeredBy})
}

// buildJobs splits what in asks for into jobs of ai.MaxItems bookmarks or ai.MaxGroups groups.
func (s *Server) buildJobs(ctx context.Context, in aiJobIn) ([]*ai.Job, error) {
	if in.Kind != ai.KindTag && in.Kind != ai.KindDedupe {
		return nil, errors.New("kind is tag or dedupe")
	}
	x, err := s.Data.Workspace(ctx, in.Workspace)
	if err != nil {
		return nil, err
	}
	t, _, err := s.Data.State(ctx, x.ID, 0)
	if err != nil {
		return nil, err
	}
	links, err := s.Data.Links(ctx, webURLs(t, nil))
	if err != nil {
		return nil, err
	}
	folder := strings.Trim(in.Folder, "/")
	newJob := func() *ai.Job {
		return &ai.Job{ID: "ai_" + newID()[3:], Kind: in.Kind, Workspace: x.ID, Folder: folder, Apply: in.Apply, State: ai.Pending, Created: s.Now().UTC()}
	}
	var jobs []*ai.Job
	switch in.Kind {
	case ai.KindTag:
		limit := in.Limit
		if limit <= 0 {
			limit = 200
		}
		if limit > 1000 {
			return nil, errors.New("limit is at most 1000 bookmarks")
		}
		vocab := map[string]int{}
		var items []ai.Item
		var found bool
		visitBookmarks(t.Root, "", func(n *store.Node, path string) {
			for _, tg := range n.Tags {
				vocab[tg]++
			}
			if folder != "" && path != folder && !strings.HasPrefix(path, folder+"/") {
				return
			}
			found = true
			if len(items) >= limit || (len(n.Tags) > 0 && !in.Retag) || !strings.HasPrefix(n.URL, "http") {
				return
			}
			it := ai.Item{ID: n.ID, Title: n.Title, URL: n.URL, Folder: path, Tags: n.Tags}
			if l, ok := links[n.URL]; ok && l.Meta != nil {
				it.PageTitle, it.Description = l.Meta.Title, l.Meta.Description
			}
			items = append(items, it)
		})
		if folder != "" && !found {
			return nil, fmt.Errorf("no bookmarks in %q", folder)
		}
		if len(items) == 0 {
			return nil, errors.New("nothing to tag: every bookmark there has tags already (retag: true includes them)")
		}
		words := make([]string, 0, len(vocab))
		for k := range vocab {
			words = append(words, k)
		}
		sort.Slice(words, func(i, j int) bool {
			return vocab[words[i]] > vocab[words[j]] || (vocab[words[i]] == vocab[words[j]] && words[i] < words[j])
		})
		if len(words) > 60 {
			words = words[:60]
		}
		for i := 0; i < len(items); i += ai.MaxItems {
			j := newJob()
			j.Items, j.Vocabulary = items[i:min(i+ai.MaxItems, len(items))], words
			jobs = append(jobs, j)
		}
	case ai.KindDedupe:
		var groups []ai.Group
		for _, g := range dupes.Find(t, links) {
			if certain(g.Reasons) {
				continue // the same address, or one leads to the other: nothing for a model to judge
			}
			ag := ai.Group{Keep: g.Keep, Reasons: g.Reasons}
			for _, m := range g.Members {
				it := ai.Item{ID: m.ID, Title: m.Title, URL: m.URL, Folder: m.Path}
				if l, ok := links[m.URL]; ok && l.Meta != nil {
					it.PageTitle, it.Description = l.Meta.Title, l.Meta.Description
				}
				ag.Members = append(ag.Members, it)
			}
			groups = append(groups, ag)
		}
		if len(groups) == 0 {
			return nil, errors.New("no uncertain duplicates to judge: the ones found share an address, or one leads to the other")
		}
		for i := 0; i < len(groups); i += ai.MaxGroups {
			j := newJob()
			j.Groups = groups[i:min(i+ai.MaxGroups, len(groups))]
			jobs = append(jobs, j)
		}
	}
	return jobs, nil
}

func certain(reasons []string) bool {
	for _, r := range reasons {
		if r == dupes.SameAddress || r == dupes.SameFinal {
			return true
		}
	}
	return false
}

// visitBookmarks calls fn for each bookmark under n with its folder path.
func visitBookmarks(n *store.Node, path string, fn func(*store.Node, string)) {
	for _, c := range n.Children {
		if c.Kind == store.KindFolder {
			p := c.Title
			if path != "" {
				p = path + "/" + c.Title
			}
			visitBookmarks(c, p, fn)
		} else {
			fn(c, path)
		}
	}
}

// runJobs answers jobs with the configured provider, one at a time.
func (s *Server) runJobs(ctx context.Context, jobs []*ai.Job) {
	for _, j := range jobs {
		s.ai.mu.Lock()
		if j.State != ai.Pending {
			s.ai.mu.Unlock()
			continue
		}
		j.State, j.Lease = ai.Working, s.Now().Add(jobLease)
		snap := *j
		s.ai.mu.Unlock()
		res, err := s.AI.Answer(ctx, snap)
		if err != nil {
			s.failJob(j, err)
			continue
		}
		_ = s.finishJob(ctx, j.ID, res, s.AI.Name())
	}
}

func (s *Server) failJob(j *ai.Job, err error) {
	s.ai.mu.Lock()
	j.State, j.Error, j.Finished = ai.Failed, err.Error(), s.Now().UTC()
	s.ai.mu.Unlock()
	_, _ = s.Bus.Publish("ai.job.failed", map[string]any{"id": j.ID, "workspace": j.Workspace, "error": err.Error()})
}

// finishJob checks an answer, keeps it on the job and, when the job says so, writes it into the workspace.
func (s *Server) finishJob(ctx context.Context, id string, res ai.Results, by string) error {
	s.ai.mu.Lock()
	j, ok := s.ai.jobs[id]
	if !ok {
		s.ai.mu.Unlock()
		return errNoJob
	}
	if j.State == ai.Done {
		s.ai.mu.Unlock()
		return errors.New("that job is answered already")
	}
	snap := *j
	s.ai.mu.Unlock()
	checked, err := ai.Check(snap, res)
	if err != nil {
		s.failJob(j, err)
		return err
	}
	var applied string
	if snap.Apply {
		e, err := s.applyResults(ctx, snap, checked)
		if err != nil {
			s.failJob(j, fmt.Errorf("the answer could not be written: %w", err))
			return err
		}
		applied = fmt.Sprintf("entry %d", e.Seq)
	}
	s.ai.mu.Lock()
	j.State, j.Results, j.By, j.Error, j.Finished = ai.Done, &checked, by, "", s.Now().UTC()
	s.ai.mu.Unlock()
	_, _ = s.Bus.Publish("ai.job.done", map[string]any{"id": id, "workspace": snap.Workspace, "kind": snap.Kind, "by": by, "applied": applied})
	return nil
}

var errNoJob = errors.New("no AI job with that id")

// applyResults writes an answer as one history entry: tags join a bookmark's own (none is removed); bookmarks judged the
// same page as the one kept go to the Trash, with the reason saying which one and that a model judged it.
func (s *Server) applyResults(ctx context.Context, j ai.Job, r ai.Results) (store.Entry, error) {
	x, err := s.Data.Workspace(ctx, j.Workspace)
	if err != nil {
		return store.Entry{}, err
	}
	t, _, err := s.Data.State(ctx, x.ID, 0)
	if err != nil {
		return store.Entry{}, err
	}
	current := map[string][]string{}
	visitBookmarks(t.Root, "", func(n *store.Node, _ string) { current[n.ID] = n.Tags })
	var ops []edit.Op
	summary := ""
	switch j.Kind {
	case ai.KindTag:
		n := 0
		for _, tr := range r.Tags {
			have, ok := current[tr.ID]
			if !ok {
				continue // gone since the job was made
			}
			ops = append(ops, edit.Op{Op: "update", ID: tr.ID, Tags: append(append([]string{}, have...), tr.Tags...)})
			n++
		}
		summary = fmt.Sprintf("Tagged %d bookmarks (suggested by a model)", n)
	case ai.KindDedupe:
		n := 0
		for _, v := range r.Verdicts {
			var ids []string
			for _, id := range v.Same {
				if _, ok := current[id]; ok {
					ids = append(ids, id)
				}
			}
			if len(ids) > 0 {
				ops = append(ops, edit.Op{Op: "trash", IDs: ids, Reason: "duplicate of " + v.Keep + " (judged the same page by a model)"})
				n += len(ids)
			}
		}
		summary = fmt.Sprintf("Moved %d duplicates to the Trash (judged by a model)", n)
	}
	if len(ops) == 0 {
		return store.Entry{Seq: x.Head}, nil
	}
	next, events, err := edit.Apply(t, ops, s.Now(), NodeID)
	if err != nil {
		return store.Entry{}, err
	}
	e, err := s.Data.Commit(ctx, x.ID, x.Head, store.Change{Op: "edit", Summary: summary, Tree: next}, s.Now())
	if err != nil {
		return store.Entry{}, err
	}
	for _, ev := range events {
		data := map[string]any{"workspace": x.ID, "seq": e.Seq}
		for k, v := range ev.Data {
			data[k] = v
		}
		_, _ = s.Bus.Publish(ev.Type, data)
	}
	_, _ = s.Bus.Publish("workspace.committed", map[string]any{"workspace": x.ID, "seq": e.Seq, "op": e.Op, "summary": e.Summary})
	return e, nil
}

// jobSummary is a job without its bookmarks, for lists.
type jobSummary struct {
	ID        string      `json:"id"`
	Kind      string      `json:"kind"`
	Workspace string      `json:"workspace"`
	Folder    string      `json:"folder,omitempty"`
	Apply     bool        `json:"apply"`
	Size      int         `json:"size"`
	State     string      `json:"state"`
	Created   time.Time   `json:"created"`
	Finished  time.Time   `json:"finished,omitzero"`
	By        string      `json:"by,omitempty"`
	Error     string      `json:"error,omitempty"`
	Results   *ai.Results `json:"results,omitempty"`
}

func summaries(jobs []*ai.Job) []jobSummary {
	out := []jobSummary{}
	for _, j := range jobs {
		out = append(out, jobSummary{j.ID, j.Kind, j.Workspace, j.Folder, j.Apply, len(j.Items) + len(j.Groups), j.State, j.Created, j.Finished, j.By, j.Error, j.Results})
	}
	return out
}

func (s *Server) listAIJobs(w http.ResponseWriter, _ *http.Request) {
	s.ai.mu.Lock()
	defer s.ai.mu.Unlock()
	var jobs []*ai.Job
	for i := len(s.ai.order) - 1; i >= 0 && len(jobs) < 100; i-- {
		jobs = append(jobs, s.ai.jobs[s.ai.order[i]])
	}
	writeJSON(w, 200, map[string]any{"jobs": summaries(jobs)})
}

// nextAIJobs hands out pending jobs (and ones whose lease ran out) to whoever answers them, such as an MCP client.
func (s *Server) nextAIJobs(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Max int    `json:"max"`
		By  string `json:"by"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Max <= 0 {
		in.Max = 1
	}
	in.Max = min(in.Max, 5)
	now := s.Now()
	s.ai.mu.Lock()
	var out []ai.Job
	for _, id := range s.ai.order {
		j := s.ai.jobs[id]
		if len(out) == in.Max {
			break
		}
		if j.State == ai.Pending || (j.State == ai.Working && now.After(j.Lease)) {
			j.State, j.Lease, j.By = ai.Working, now.Add(jobLease), in.By
			out = append(out, *j)
		}
	}
	s.ai.mu.Unlock()
	if out == nil {
		out = []ai.Job{}
	}
	writeJSON(w, 200, map[string]any{"jobs": out, "schema": map[string]any{ai.KindTag: ai.Schema(ai.KindTag), ai.KindDedupe: ai.Schema(ai.KindDedupe)}})
}

// submitAIResults takes an answer to a job.
func (s *Server) submitAIResults(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Results ai.Results `json:"results"`
		By      string     `json:"by"`
	}
	if !decode(w, r, &in) {
		return
	}
	by := in.By
	if by == "" {
		by = "mcp"
	}
	err := s.finishJob(r.Context(), r.PathValue("id"), in.Results, by)
	switch {
	case errors.Is(err, errNoJob):
		writeErr(w, 404, err.Error())
		return
	case err != nil:
		writeErr(w, 400, err.Error())
		return
	}
	s.ai.mu.Lock()
	j := *s.ai.jobs[r.PathValue("id")]
	s.ai.mu.Unlock()
	writeJSON(w, 200, map[string]any{"job": summaries([]*ai.Job{&j})[0]})
}

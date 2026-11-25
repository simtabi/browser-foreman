package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/ai"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// atWorkspace makes two answers meet after each read the workspace to apply into, so without the claim both apply.
type atWorkspace struct{ slowReads }

func (r atWorkspace) Workspace(ctx context.Context, id string) (store.Workspace, error) {
	w, err := r.Store.Workspace(ctx, id)
	r.meet()
	return w, err
}

// Two answers to one job at once (a provider and an MCP client) write it into the workspace once, not twice.
func TestAnAIJobIsAppliedOnce(t *testing.T) {
	s := seeded(t)
	s.aiLock()
	s.ai.add(&ai.Job{ID: "ai_once", Kind: ai.KindTag, Workspace: "main", Apply: true, State: ai.Working,
		Items: []ai.Item{{ID: "go", Title: "Go", URL: "https://go.dev/"}}, Created: time.Now()})
	s.aiSaveUnlock()
	s.Data = atWorkspace{newSlowReads(s.Data)}
	res := ai.Results{Tags: []ai.TagResult{{ID: "go", Tags: []string{"golang"}}}}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errs[i] = s.finishJob(context.Background(), "ai_once", res, "test") }()
	}
	wg.Wait()
	h, err := s.Data.History(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 {
		t.Fatalf("%d history entries after two answers (want the import and one AI entry); errors %v", len(h), errs)
	}
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("exactly one answer should be taken: %v", errs)
	}
	s.aiLock()
	state := s.ai.jobs["ai_once"].State
	s.ai.mu.Unlock()
	if state != ai.Done {
		t.Fatalf("the job is %s after one answer was applied: the refused second one undid it", state)
	}
}

// A provider failing after the job was answered (it timed out, while an MCP client answered) does not undo it.
func TestALateFailureDoesNotUndoAnAnswer(t *testing.T) {
	s := seeded(t)
	j := &ai.Job{ID: "ai_done", Kind: ai.KindTag, Workspace: "main", State: ai.Done, Created: time.Now()}
	s.aiLock()
	s.ai.add(j)
	s.aiSaveUnlock()
	s.failJob(j, context.DeadlineExceeded)
	if j.State != ai.Done {
		t.Fatalf("state %s", j.State)
	}
}

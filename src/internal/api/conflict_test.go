package api

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// racedStore stands for a store another writer always gets to first: every commit conflicts, and the workspace is a
// step further on each time it is read.
type racedStore struct {
	store.Store
	reads int
}

func (r *racedStore) Commit(context.Context, string, int, store.Change, time.Time) (store.Entry, error) {
	return store.Entry{}, store.ErrConflict
}

func (r *racedStore) Workspace(ctx context.Context, id string) (store.Workspace, error) {
	w, err := r.Store.Workspace(ctx, id)
	r.reads++
	w.Head += r.reads
	return w, err
}

// Undo or redo that loses a race to another change answers 409, as an edit does, not 500.
func TestUndoThatLosesARaceIsAConflict(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	tr, _ := tree(t, h)
	tr.Root.Children[0].Children[0].Title = "changed"
	body, _ := json.Marshal(tr)
	if w := send(h, "PUT", "/api/v1/workspaces/main/tree", string(body)); w.Code != 200 && w.Code != 201 {
		t.Fatalf("edit %d %s", w.Code, w.Body)
	}
	real := s.Data
	s.Data = &racedStore{Store: real}
	if w := send(s.Handler(), "POST", "/api/v1/workspaces/main/undo", ""); w.Code != 409 {
		t.Errorf("undo: %d %s", w.Code, w.Body)
	}
	s.Data = real
	if w := send(s.Handler(), "POST", "/api/v1/workspaces/main/undo", ""); w.Code != 201 {
		t.Fatalf("a real undo: %d %s", w.Code, w.Body)
	}
	s.Data = &racedStore{Store: real}
	if w := send(s.Handler(), "POST", "/api/v1/workspaces/main/redo", ""); w.Code != 409 {
		t.Errorf("redo: %d %s", w.Code, w.Body)
	}
}

// The conflict message says where the workspace is now, not where it was when the request began.
func TestAConflictReportsTheHeadAsItIsNow(t *testing.T) {
	s := seeded(t)
	tr, _ := tree(t, s.Handler())
	tr.Root.Children[0].Children[0].Title = "changed"
	body, _ := json.Marshal(tr)
	s.Data = &racedStore{Store: s.Data}
	w := send(s.Handler(), "PUT", "/api/v1/workspaces/main/tree", string(body))
	m := regexp.MustCompile(`is at (\d+) now, not (\d+)`).FindStringSubmatch(w.Body.String())
	if w.Code != 409 || m == nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if m[1] == m[2] {
		t.Fatalf("it reports the head read before the commit, not the one after: %s", w.Body)
	}
}

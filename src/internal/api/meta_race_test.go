package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// slowReads makes two requests meet: after a read, each waits until both have read (or 200 ms pass), so without a
// lock both hold what they read before either writes, every time. With the lock the second cannot arrive while the first holds it,
// and the first goes on after the wait.
type slowReads struct {
	store.Store
	mu      *sync.Mutex
	arrived *int
	both    chan struct{}
}

func newSlowReads(s store.Store) slowReads {
	return slowReads{Store: s, mu: &sync.Mutex{}, arrived: new(int), both: make(chan struct{})}
}

func (r slowReads) meet() {
	r.mu.Lock()
	*r.arrived++
	if *r.arrived == 2 {
		close(r.both)
	}
	r.mu.Unlock()
	select {
	case <-r.both:
	case <-time.After(200 * time.Millisecond):
	}
}

// atList makes the requests meet where a delete counts the workspaces in use; atOne where each reads its workspace.
type atList struct{ slowReads }

func (r atList) Workspaces(ctx context.Context) ([]store.Workspace, error) {
	ws, err := r.Store.Workspaces(ctx) // read first, then wait for the other: both hold what they read before either writes
	r.meet()
	return ws, err
}

type atOne struct{ slowReads }

func (r atOne) Workspace(ctx context.Context, id string) (store.Workspace, error) {
	w, err := r.Store.Workspace(ctx, id)
	r.meet()
	return w, err
}

// Two workspaces deleted at the same moment: one goes to the Trash, the other stays, because one workspace must always
// stay in use. Each delete used to check before the other wrote, so both could go.
func TestTwoDeletesAtOnceKeepOneWorkspace(t *testing.T) {
	for round := range 5 {
		s := newServer(t, apiTok)
		ctx := context.Background()
		for _, id := range []string{"a", "b"} {
			if _, err := s.Data.CreateWorkspace(ctx, id, id, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		s.Data = atList{newSlowReads(s.Data)}
		h := s.Handler()
		var wg sync.WaitGroup
		for _, id := range []string{"a", "b"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				send(h, "POST", "/api/v1/workspaces/"+id+"/delete", `{"confirm":"`+id+`"}`)
			}()
		}
		wg.Wait()
		ws, _ := s.Data.Workspaces(ctx)
		live := 0
		for _, w := range ws {
			if w.Deleted.IsZero() {
				live++
			}
		}
		if live != 1 {
			t.Fatalf("round %d: %d workspaces left in use", round, live)
		}
	}
}

// A rename racing a delete: whichever answers 200 has its change kept. Each wrote back the whole record it read, so
// the rename could bring the workspace back out of the Trash, or the delete undo the rename.
func TestARenameAndADeleteKeepEachOthersChanges(t *testing.T) {
	for round := range 5 {
		s := newServer(t, apiTok)
		ctx := context.Background()
		for _, id := range []string{"keep", "x"} {
			_, _ = s.Data.CreateWorkspace(ctx, id, id, time.Now())
		}
		s.Data = atOne{newSlowReads(s.Data)}
		h := s.Handler()
		var wg sync.WaitGroup
		var del, ren int
		wg.Add(2)
		go func() { defer wg.Done(); del = send(h, "POST", "/api/v1/workspaces/x/delete", `{"confirm":"x"}`).Code }()
		go func() { defer wg.Done(); ren = send(h, "PATCH", "/api/v1/workspaces/x", `{"name":"renamed"}`).Code }()
		wg.Wait()
		x, err := s.Data.(atOne).Store.Workspace(ctx, "x")
		if err != nil {
			t.Fatal(err)
		}
		if del == 200 && x.Deleted.IsZero() {
			t.Fatalf("round %d: the delete answered 200 but was undone: %+v", round, x)
		}
		if ren == 200 && x.Name != "renamed" {
			t.Fatalf("round %d: the rename answered 200 but was lost: %+v", round, x)
		}
	}
}

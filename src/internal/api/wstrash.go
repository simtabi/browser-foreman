package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// startOps are the changes a workspace can start from: Reset goes back to the first of them.
var startOps = map[string]bool{"import": true, "merge": true, "start": true} // "start": the Python tool's first entry

// resetWorkspace makes the workspace as it started (its first import or merge; empty when it had none) the newest
// state, as one entry: undo or History brings everything back.
func (s *Server) resetWorkspace(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	saw, given, err := base(r, nil)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	h, err := s.Data.History(r.Context(), x.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(h) == 0 {
		writeErr(w, 409, "this workspace has no changes to go back from")
		return
	}
	start := h[0]
	for _, e := range h {
		if startOps[e.Op] {
			start = e
			break
		}
	}
	t, _, err := s.Data.State(r.Context(), x.ID, start.Seq)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	summary := fmt.Sprintf("Reset to how it started (change %d: %s)", start.Seq, start.Summary)
	s.commitWith(w, r, x, saw, given, given, store.Change{Op: "reset", Summary: summary, Tree: t}, nil, "reset", nil, nil)
}

// confirmed checks a {confirm} body naming the workspace's id, for the steps that need it typed.
func confirmed(w http.ResponseWriter, r *http.Request, x store.Workspace) bool {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return false
	}
	if in.Confirm != x.ID {
		writeErr(w, 400, fmt.Sprintf("confirm with the workspace's id: {\"confirm\": %q}", x.ID))
		return false
	}
	return true
}

// deleteWorkspace moves a workspace to the workspace Trash. Nothing is removed: restore brings it back whole. A locked
// workspace, and the last one in use, cannot be deleted.
func (s *Server) deleteWorkspace(w http.ResponseWriter, r *http.Request) {
	s.meta.Lock() // read, check and write as one step: see Server.meta
	defer s.meta.Unlock()
	x, ok := s.workspace(w, r)
	if !ok || !confirmed(w, r, x) {
		return
	}
	if x.Locked {
		writeErr(w, http.StatusLocked, "the workspace is locked; unlock it to delete it")
		return
	}
	all, err := s.Data.Workspaces(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	live := 0
	for _, a := range all {
		if a.Deleted.IsZero() {
			live++
		}
	}
	if live <= 1 {
		writeErr(w, 409, "this is the only workspace in use; make or restore another first")
		return
	}
	x.Deleted = s.Now().UTC()
	if err := s.Data.UpdateWorkspace(r.Context(), x); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, _ = s.Bus.Publish("workspace.deleted", map[string]any{"workspace": x.ID, "name": x.Name})
	writeJSON(w, 200, map[string]any{"workspace": x})
}

// trashed finds a workspace that is in the workspace Trash.
func (s *Server) trashed(w http.ResponseWriter, r *http.Request) (store.Workspace, bool) {
	x, err := s.Data.Workspace(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return x, false
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return x, false
	}
	if x.Deleted.IsZero() {
		writeErr(w, 409, "that workspace is not in the workspace Trash")
		return x, false
	}
	return x, true
}

// restoreWorkspace brings a workspace back from the workspace Trash, history and all.
func (s *Server) restoreWorkspace(w http.ResponseWriter, r *http.Request) {
	s.meta.Lock() // read, check and write as one step: see Server.meta
	defer s.meta.Unlock()
	x, ok := s.trashed(w, r)
	if !ok {
		return
	}
	x.Deleted = store.Workspace{}.Deleted
	if err := s.Data.UpdateWorkspace(r.Context(), x); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, _ = s.Bus.Publish("workspace.restored", map[string]any{"workspace": x.ID, "name": x.Name})
	writeJSON(w, 200, map[string]any{"workspace": x})
}

// purgeWorkspace removes a workspace in the workspace Trash for good. It cannot be undone, so it needs the id typed.
func (s *Server) purgeWorkspace(w http.ResponseWriter, r *http.Request) {
	s.meta.Lock() // read, check and write as one step: see Server.meta
	defer s.meta.Unlock()
	x, ok := s.trashed(w, r)
	if !ok || !confirmed(w, r, x) {
		return
	}
	if err := s.Data.PurgeWorkspace(r.Context(), x.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, _ = s.Bus.Publish("workspace.purged", map[string]any{"workspace": x.ID, "name": x.Name})
	writeJSON(w, 200, map[string]any{"purged": x.ID})
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/writeback"
)

// serverExtension carries commands to a browser's extension through this server's own queue (the command line goes
// through the API instead), so the page can preview and apply a write-back.
type serverExtension struct{ s *Server }

func (x serverExtension) Run(ctx context.Context, browser string, cmd map[string]any, what string, wait time.Duration) (json.RawMessage, error) {
	s := x.s
	c := &command{Browser: browser, State: "queued", Created: s.Now().UTC(), ID: "cmd_" + newID()[3:]}
	c.Kind, _ = cmd["kind"].(string)
	c.Workspace, _ = cmd["workspace"].(string)
	c.Expect, _ = cmd["expect"].(string)
	s.ext.mu.Lock()
	s.ext.init()
	s.ext.prune(s.Now())
	s.ext.commands[browser] = append(s.ext.commands[browser], c)
	s.ext.byID[c.ID] = c
	close(s.ext.wake) // wake the waiting extension
	s.ext.wake = make(chan struct{})
	s.ext.mu.Unlock()
	deadline := time.After(wait)
	for {
		s.ext.mu.Lock()
		state, msg, res := c.State, c.Error, c.Result
		s.ext.mu.Unlock()
		switch state {
		case "done":
			return res, nil
		case "failed":
			return nil, fmt.Errorf("%s's extension could not %s: %s", browser, what, msg)
		}
		select {
		case <-ctx.Done():
			return nil, x.withdraw(c, ctx.Err())
		case <-deadline:
			return nil, x.withdraw(c, fmt.Errorf("%s's extension did not answer within %s: is it connected, and is %s open?", browser, wait, browser))
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// withdraw takes back a command its caller stopped waiting for. One still queued is cancelled, so the extension never
// runs it after the person was told it failed (it would otherwise wait in the queue for an hour, and a write-back could
// still be applied with no read-back or drift check). One already sent may still run, and the error says so.
func (x serverExtension) withdraw(c *command, why error) error {
	s := x.s
	s.ext.mu.Lock()
	defer s.ext.mu.Unlock()
	switch c.State {
	case "queued":
		c.State, c.Error = "failed", "cancelled: nobody was waiting for it any more"
		return why
	case "sent":
		return fmt.Errorf("%w; the extension had already taken it, so it may still be applied: check the browser", why)
	}
	return why
}

// writebackIn is a write-back as the page asks for it: always through the browser's extension.
type writebackIn struct {
	Workspace string `json:"workspace"`
	Base      string `json:"base"`
	Browser   string `json:"browser"` // as its extension names it (Google Chrome)
	Mode      string `json:"mode"`
	Expect    string `json:"expect"`   // apply: the browser's fingerprint the preview was made against
	DropNew   int    `json:"drop_new"` // apply, replace: how many bookmarks the browser gained that it may drop
}

func (in writebackIn) job() (writeback.Job, error) {
	m := writeback.Mode(in.Mode)
	if m != writeback.Merge && m != writeback.Replace {
		return writeback.Job{}, errors.New("mode is merge or replace")
	}
	if strings.TrimSpace(in.Workspace) == "" || strings.TrimSpace(in.Base) == "" || strings.TrimSpace(in.Browser) == "" {
		return writeback.Job{}, errors.New("name the workspace to write, the export it started from (base) and the browser")
	}
	return writeback.Job{Workspace: in.Workspace, Base: in.Base, Mode: m, DropNew: in.DropNew}, nil
}

// previewWriteback reads the browser now through its extension and plans the write: nothing is written. Owner only.
func (s *Server) previewWriteback(w http.ResponseWriter, r *http.Request) {
	var in writebackIn
	if !decode(w, r, &in) {
		return
	}
	j, err := in.job()
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	x := serverExtension{s}
	theirs, err := writeback.ReadNow(r.Context(), s.Data, x, in.Browser)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	_, rep, err := writeback.PlanTree(r.Context(), s.Data, j, theirs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"report": rep, "expect": writeback.ExtFingerprint(theirs)})
}

// applyWriteback reads the browser again, refuses if it changed since the preview, and writes the plan through its
// extension, which saves the browser first; then reads it back and plans again to verify. Owner only.
func (s *Server) applyWriteback(w http.ResponseWriter, r *http.Request) {
	var in writebackIn
	if !decode(w, r, &in) {
		return
	}
	j, err := in.job()
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	x := serverExtension{s}
	theirs, err := writeback.ReadNow(r.Context(), s.Data, x, in.Browser)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	if in.Expect == "" || writeback.ExtFingerprint(theirs) != in.Expect {
		writeErr(w, 409, in.Browser+"'s bookmarks changed since the preview: preview again before writing")
		return
	}
	target, rep, err := writeback.PlanTree(r.Context(), s.Data, j, theirs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if j.Mode == writeback.Replace && rep.DropNew != j.DropNew {
		writeErr(w, 400, fmt.Sprintf("replace removes %d bookmarks the browser gained after the export: confirm that number", rep.DropNew))
		return
	}
	res, err := writeback.WriteThrough(r.Context(), s.Data, x, j, in.Browser, theirs, target, writeback.Slug(in.Browser)+"-live", s.Now())
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": err.Error(), "written": res})
		return
	}
	writeJSON(w, 200, map[string]any{"written": res})
}

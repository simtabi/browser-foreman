package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/treesync"
)

// syncTarget reads a sync request's workspace: one this extension may write (read from its browser), with history.
func (s *Server) syncTarget(w http.ResponseWriter, r *http.Request, browser, id string) (store.Workspace, bool) {
	if store.RoleRank(principal(r).Role) < store.RoleRank(store.RoleEditor) {
		writeErr(w, 403, "this needs the editor role")
		return store.Workspace{}, false
	}
	if ok, err := s.extensionWritable(r.Context(), browser, id); err != nil {
		writeErr(w, 500, err.Error())
		return store.Workspace{}, false
	} else if !ok {
		writeErr(w, 403, "an extension syncs only a workspace read from its own browser")
		return store.Workspace{}, false
	}
	r.SetPathValue("id", id)
	return s.workspace(w, r)
}

// syncBookmarks merges the browser's bookmarks into a workspace read from it, against the entry both last agreed on
// (base), and answers with the merged tree for the extension to write into the browser. Where both changed one
// thing, the side changed last wins: the browser's last bookmark event (changed, Unix milliseconds) against the
// workspace's last entry. What the browser removed goes to the workspace's Trash.
func (s *Server) syncBookmarks(w http.ResponseWriter, r *http.Request, browser string) {
	var in struct {
		Workspace string             `json:"workspace"`
		Base      int                `json:"base"`
		Changed   int64              `json:"changed"`
		Nodes     []importer.ExtNode `json:"nodes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, importer.MaxSize)).Decode(&in); err != nil {
		writeErr(w, 400, "bad JSON body: "+err.Error())
		return
	}
	theirs, err := importer.FromExtension(in.Nodes)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	x, ok := s.syncTarget(w, r, browser, in.Workspace)
	if !ok {
		return
	}
	if in.Base == 0 { // the first sync: from the last time this browser and the workspace agreed
		if in.Base, err = s.lastAgreed(r, x.ID, browser); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
	}
	if in.Base < 1 || in.Base > x.Head {
		writeErr(w, 400, fmt.Sprintf("base must be an entry of the workspace (1 to %d): the one this browser last synced", x.Head))
		return
	}
	base, _, err := s.Data.State(r.Context(), x.ID, in.Base)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	head, entry, err := s.Data.State(r.Context(), x.ID, 0)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	newer := treesync.Workspace
	if in.Changed > 0 && !time.UnixMilli(in.Changed).Before(entry.At) {
		newer = treesync.Browser
	}
	res := treesync.Merge3(base, head, theirs, newer, s.Now().Unix(), "removed in "+browser)
	merged, _, _ := store.Hash(res.Tree)
	now, _, _ := store.Hash(head)
	answer := map[string]any{"tree": res.Tree, "conflicts": res.Conflicts, "removed": res.Removed}
	if merged == now {
		answer["seq"] = x.Head
		writeJSON(w, 200, answer)
		return
	}
	summary := fmt.Sprintf("Synced with %s", browser)
	if res.Conflicts > 0 || res.Removed > 0 {
		summary += fmt.Sprintf(": %d conflict(s) settled for the newer side, %d removed to the Trash", res.Conflicts, res.Removed)
	}
	s.commitWith(w, r, x, x.Head, true, false, store.Change{Op: "sync", Summary: summary, Tree: res.Tree}, nil, "committed", answer, nil)
}

// adoptBookmarks gives the workspace the browser's ids after the merge entry seq was written into the browser:
// nodes, the browser's bookmarks read back, hold what it made under new ids. Only ids change, on the workspace as it
// is now, so an edit made in Browser Foreman meanwhile is kept and nothing is refused.
func (s *Server) adoptBookmarks(w http.ResponseWriter, r *http.Request, browser string) {
	var in struct {
		Workspace string             `json:"workspace"`
		Seq       int                `json:"seq"`
		Nodes     []importer.ExtNode `json:"nodes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, importer.MaxSize)).Decode(&in); err != nil {
		writeErr(w, 400, "bad JSON body: "+err.Error())
		return
	}
	read, err := importer.FromExtension(in.Nodes)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	x, ok := s.syncTarget(w, r, browser, in.Workspace)
	if !ok {
		return
	}
	if in.Seq < 1 || in.Seq > x.Head {
		writeErr(w, 400, fmt.Sprintf("seq must be an entry of the workspace (1 to %d): the merge written into the browser", x.Head))
		return
	}
	written, _, err := s.Data.State(r.Context(), x.ID, in.Seq)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	ids := treesync.NewIDs(written, read)
	if len(ids) == 0 {
		writeJSON(w, 200, map[string]any{"seq": x.Head})
		return
	}
	head, _, err := s.Data.State(r.Context(), x.ID, 0)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	next := treesync.Rename(head, ids)
	summary := fmt.Sprintf("Took %s's ids for %d it made", browser, len(ids))
	s.commitWith(w, r, x, x.Head, true, false, store.Change{Op: "sync", Summary: summary, Tree: next}, nil, "committed", nil, nil)
}

// lastAgreed is the last entry where the browser and the workspace held the same bookmarks: the last read from the
// browser (a write-back reads the result back too), or the last sync with it.
func (s *Server) lastAgreed(r *http.Request, id, browser string) (int, error) {
	hist, err := s.Data.History(r.Context(), id)
	if err != nil {
		return 0, err
	}
	for i := len(hist) - 1; i >= 0; i-- {
		e := hist[i]
		if (e.Op == "import" && strings.HasSuffix(e.Summary, " from "+browser+" (extension)")) ||
			(e.Op == "sync" && (strings.HasPrefix(e.Summary, "Synced with "+browser) || strings.HasPrefix(e.Summary, "Took "+browser+"'s ids"))) {
			return e.Seq, nil
		}
	}
	return 0, fmt.Errorf("this workspace was never read from %s: read it first", browser)
}

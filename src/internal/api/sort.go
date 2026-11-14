package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/simtabi/browser-foreman/src/internal/edit"
	"github.com/simtabi/browser-foreman/src/internal/sorting"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// SortRulesFile is where the owner's filing rules live, in the data folder: they name the owner's own folders and
// sites, so they are data, never part of the program.
const SortRulesFile = "sort-rules.json"

// ReadSortRules reads the filing rules from dir (the data folder). fs.ErrNotExist when there are none yet.
func ReadSortRules(dir string) (sorting.Rules, error) {
	var r sorting.Rules
	b, err := os.ReadFile(filepath.Join(dir, SortRulesFile))
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s: %w", SortRulesFile, err)
	}
	return r, nil
}

// SortSummary is the one line a sort's history entry says.
func SortSummary(p sorting.Plan) string {
	plural := func(n int, one, many string) string {
		if n == 1 {
			return fmt.Sprint(n, " ", one)
		}
		return fmt.Sprint(n, " ", many)
	}
	return fmt.Sprintf("Sorted %s into folders (%s)", plural(len(p.Moves), "bookmark", "bookmarks"), plural(len(p.New), "new folder", "new folders"))
}

// sortWorkspace files a workspace's bookmarks by the owner's rules (sorting.Rules). Without "apply" it only says
// where each would go; with it, the moves are one history entry, undone like any other.
func (s *Server) sortWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Apply bool `json:"apply"`
		Base  *int `json:"base"`
		// Folders, when given, limits the moves to those destinations (the ones the owner ticked).
		Folders []string `json:"folders"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	rules, err := ReadSortRules(s.Cfg.Storage.Path)
	if errors.Is(err, fs.ErrNotExist) {
		writeErr(w, 409, "no filing rules yet: write them to "+filepath.Join(s.Cfg.Storage.Path, SortRulesFile)+" (see docs/tools/sorting.md)")
		return
	}
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	links, err := s.Data.Links(r.Context(), webURLs(t, nil))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	plan, err := rules.Plan(t, links)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.Folders != nil {
		plan = plan.Only(in.Folders)
	}
	if !in.Apply || len(plan.Moves) == 0 {
		writeJSON(w, 200, map[string]any{"plan": plan, "entry": nil})
		return
	}
	saw, given, err := base(r, in.Base)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	next, err := sorting.Apply(t, plan, NodeID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	events, err := edit.Diff(t, next)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.commitWith(w, r, x, saw, given, false, store.Change{Op: "sort", Summary: SortSummary(plan), Tree: next}, events, "committed",
		map[string]any{"plan": plan}, nil)
}

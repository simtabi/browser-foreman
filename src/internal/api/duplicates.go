package api

import (
	"net/http"

	"github.com/simtabi/browser-foreman/src/internal/dupes"
)

// findDuplicates lists a workspace's bookmarks that lead to the same page (dupes.Find), using the latest link checks
// for where links end up and which address each page names as its own. It changes nothing: moving the copies to the
// Trash is a trash change with the reason "duplicate of <kept id>", so they join the merged copies in Duplicates.
func (s *Server) findDuplicates(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
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
	groups := dupes.Find(t, links)
	copies := 0
	for _, g := range groups {
		copies += len(g.Members) - 1
	}
	if groups == nil {
		groups = []dupes.Group{}
	}
	writeJSON(w, 200, map[string]any{"groups": groups, "copies": copies})
}

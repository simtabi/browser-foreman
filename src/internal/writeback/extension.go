package writeback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Extension carries a command to a browser's extension and waits for it: the command line through the running
// server's API, the server through its own queue. It returns what the extension reported.
type Extension interface {
	Run(ctx context.Context, browser string, cmd map[string]any, what string, wait time.Duration) (json.RawMessage, error)
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is a browser's name as its extension turns it into a workspace id ("Google Chrome" → "google-chrome").
func Slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// ReadNow has the browser's extension read its bookmarks into <browser>-live now and returns them.
func ReadNow(ctx context.Context, s store.Store, x Extension, browser string) (store.Tree, error) {
	if _, err := x.Run(ctx, browser, map[string]any{"kind": "read-bookmarks"}, "read its bookmarks", 2*time.Minute); err != nil { // it takes commands at least once a minute
		return store.Tree{}, err
	}
	t, _, err := s.State(ctx, Slug(browser)+"-live", 0)
	return t, err
}

// Written is what writing through the extension did, and what planning again found.
type Written struct {
	Created          int    `json:"created"`
	Updated          int    `json:"updated"`
	Moved            int    `json:"moved"`
	RemovedBookmarks int    `json:"removedBookmarks"`
	RemovedFolders   int    `json:"removedFolders"`
	Steps            int    `json:"steps"`
	Backup           string `json:"backup"`
	Target           int    `json:"target_entry"` // the history entry the plan was saved as, in <browser>-target
	Drift            Report `json:"drift"`        // planning again after the write: empty when it landed as planned
}

// WriteThrough saves the plan's target as <browser>-target and has the browser's extension write it, refusing in
// the extension if the browser no longer matches theirs (the tree the plan was made against). The extension saves the
// browser first and reads the result back; then the browser is read again and planned against: Written.Drift is empty
// when the write landed as planned.
func WriteThrough(ctx context.Context, s store.Store, x Extension, j Job, browser string, theirs, target store.Tree, from string, now time.Time) (Written, error) {
	name := Slug(browser) + "-target"
	e, err := Save(ctx, s, name, browser+" write-back", "writeback", fmt.Sprintf("Write-back of %s (%s) planned against %s", j.Workspace, j.Mode, from), target, now)
	if err != nil {
		return Written{}, err
	}
	raw, err := x.Run(ctx, browser, map[string]any{"kind": "write-bookmarks", "workspace": name, "expect": ExtFingerprint(theirs)}, "write the plan", 20*time.Minute)
	if err != nil {
		return Written{}, err
	}
	var w Written
	_ = json.Unmarshal(raw, &w)
	w.Target = e.Seq
	after, err := ReadNow(ctx, s, x, browser)
	if err != nil {
		return w, fmt.Errorf("written, but reading it back failed: %w", err)
	}
	if _, w.Drift, err = PlanTree(ctx, s, j, after); err != nil {
		return w, err
	}
	if !w.Drift.Empty() {
		return w, errors.New("the browser's bookmarks differ from the plan after writing")
	}
	return w, nil
}

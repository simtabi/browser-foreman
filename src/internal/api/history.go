package api

import (
	"errors"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/history"
	"github.com/simtabi/browser-foreman/src/internal/merge"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// blockedWhy is why a history file the system keeps closed is left out.
var blockedWhy = map[bool]string{true: "macOS keeps it behind Full Disk Access, which the app running Browser Foreman does not have",
	false: "the system does not let this program read it"}[runtime.GOOS == "darwin"]

// historyFiles is where history files are looked for; tests replace it.
var historyFiles = history.Find

// historyAccess says, per browser profile, whether its history can be read. It opens nothing beyond one byte.
func (s *Server) historyAccess(w http.ResponseWriter, _ *http.Request) {
	files := historyFiles()
	present, blocked := 0, 0
	type row struct {
		Browser string `json:"browser"`
		Profile string `json:"profile,omitempty"`
		State   string `json:"state"`
	}
	rows := []row{}
	for _, f := range files {
		rows = append(rows, row{f.Browser, f.Profile, f.State})
		if f.State != "missing" {
			present++
		}
		if f.State == "blocked" {
			blocked++
		}
	}
	bin, _ := os.Executable()
	launched := os.Getenv("TERM_PROGRAM")
	if launched == "" {
		launched = os.Getenv("__CFBundleIdentifier")
	}
	writeJSON(w, 200, map[string]any{"browsers": rows, "granted": present > 0 && blocked == 0, "blocked": blocked,
		"binary": bin, "launched_by": launched, "settings_url": history.SettingsURL})
}

// openAccessSettings opens System Settings at Full Disk Access (macOS). The owner grants it there; nothing is
// granted from here.
func (s *Server) openAccessSettings(w http.ResponseWriter, r *http.Request) {
	if history.SettingsURL == "" || s.Browsers == nil || s.Browsers.Platform == nil {
		writeErr(w, 400, "this system has no Full Disk Access setting to open")
		return
	}
	if err := s.Browsers.Platform.Launch(r.Context(), []string{"open", history.SettingsURL}); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// searchHistory answers the pages visited, newest first, one row per address across browsers: ?q= words,
// ?days= (default 30), ?browser=, ?limit= (default 300). With ?workspace= each row says whether that workspace has a
// bookmark for the page. History is read, never stored, and never published as an event.
func (s *Server) searchHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	days, limit := 30, 300
	if v := q.Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 3650 {
			writeErr(w, 400, "days: 1 to 3650")
			return
		}
		days = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 5000 {
			writeErr(w, 400, "limit: 1 to 5000")
			return
		}
		limit = n
	}
	var have map[string]bool
	if id := q.Get("workspace"); id != "" {
		x, err := s.Data.Workspace(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "no workspace with that id")
			return
		}
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		t, err := s.head(r, x)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		have = map[string]bool{}
		var walk func(n *store.Node)
		walk = func(n *store.Node) {
			for _, c := range n.Children {
				if c.Kind == store.KindFolder {
					walk(c)
				} else {
					have[merge.Key(c.URL)] = true
				}
			}
		}
		walk(t.Root)
	}
	hq := history.Query{Words: strings.Fields(strings.ToLower(q.Get("q"))), Since: s.Now().AddDate(0, 0, -days), Limit: limit}
	type skipped struct{ Browser, Profile, Why string }
	var skip []skipped
	read := 0
	byURL := map[string]*history.Visit{}
	for _, f := range historyFiles() {
		if b := q.Get("browser"); b != "" && !strings.EqualFold(b, f.Browser) {
			continue
		}
		if f.State != "readable" {
			if f.State == "blocked" {
				skip = append(skip, skipped{f.Browser, f.Profile, blockedWhy})
			}
			continue
		}
		vs, err := history.Read(r.Context(), f, hq)
		if err != nil {
			skip = append(skip, skipped{f.Browser, f.Profile, err.Error()})
			continue
		}
		read++
		for _, v := range vs {
			if old, ok := byURL[v.URL]; ok {
				old.Visits += v.Visits
				if v.Last.After(old.Last) {
					old.Last, old.Browser, old.Profile = v.Last, v.Browser, v.Profile
					if v.Title != "" {
						old.Title = v.Title
					}
				}
				continue
			}
			c := v
			byURL[v.URL] = &c
		}
	}
	type out struct {
		history.Visit
		Bookmarked *bool `json:"bookmarked,omitempty"`
	}
	all := make([]out, 0, len(byURL))
	for _, v := range byURL {
		o := out{Visit: *v}
		if have != nil {
			b := have[merge.Key(v.URL)]
			o.Bookmarked = &b
		}
		all = append(all, o)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Last.After(all[j].Last) })
	if len(all) > limit {
		all = all[:limit]
	}
	if skip == nil {
		skip = []skipped{}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"visits": all, "read": read, "skipped": skip, "since": hq.Since.Format(time.RFC3339)})
}

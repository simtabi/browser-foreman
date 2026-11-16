package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

// Prefs are the owner's choices about browsers and tabs, kept with the data (not in the config files, which hold the
// server's own settings). Every field has a default, so a store that has never saved them reads as Defaults.
type Prefs struct {
	// OpenWith: the browsers links open in; "System default" follows the system's choice.
	OpenWith []string `json:"open_with"`
	// Tracked: the browsers whose tabs are read; nil reads every scriptable one installed.
	Tracked []string `json:"tracked,omitempty"`
	// Autosave takes tab snapshots on its own: every IntervalSeconds, or on any change (SaveMode "change", checked
	// every WatchSeconds).
	Autosave        bool   `json:"autosave"`
	SaveMode        string `json:"save_mode"`
	IntervalSeconds int    `json:"interval_seconds"`
	WatchSeconds    int    `json:"watch_seconds"`
	// IncludeIncognito keeps private windows' pages in snapshots; otherwise only how many tabs they had.
	IncludeIncognito bool `json:"include_incognito"`
	// IncognitoPurgeHours removes the pages of private windows older than this many hours after each save (their
	// counts stay); 0 never does.
	IncognitoPurgeHours int `json:"incognito_purge_hours"`
	// KeepDays: snapshots older than this are hidden from lists (never deleted).
	KeepDays int `json:"keep_days"`
	// RecheckDays re-checks every bookmarked link this often; 0 is off.
	RecheckDays int `json:"recheck_days"`
	// Wayback asks the Internet Archive for a copy of each link once it is confirmed dead. Off by default: it sends
	// those addresses to archive.org.
	Wayback bool `json:"wayback"`
	// RenderChecks opens links a plain request could not settle (bot checks, 403s, timeouts) in headless Chrome after
	// each run, and judges what Chrome shows. On by default; it needs a Chromium browser installed.
	RenderChecks bool `json:"render_checks"`
	// Page snapshots: the browser window they are taken in (a taller one shows more of the page), how long a page
	// may run before the picture is taken, and the folder they are kept in ("": the data folder's media/).
	SnapshotWidth       int    `json:"snapshot_width"`
	SnapshotHeight      int    `json:"snapshot_height"`
	SnapshotWaitSeconds int    `json:"snapshot_wait_seconds"`
	SnapshotDir         string `json:"snapshot_dir"`
	// Tab snapshots per workspace: TabsOwn lists the workspaces that keep their own; the rest share one pool.
	// ActiveWorkspace is the workspace last opened, which background snapshots are saved for.
	TabsOwn         []string `json:"tabs_own,omitempty"`
	ActiveWorkspace string   `json:"active_workspace,omitempty"`
	// SmartFolders are saved searches: a name over a Show filter and search text.
	SmartFolders []SmartFolder `json:"smart_folders,omitempty"`
}

// SmartFolder is one saved search. Workspace limits it to one workspace; empty shows it in every one.
type SmartFolder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Workspace string `json:"workspace,omitempty"`
	Q         string `json:"q,omitempty"`
	Filter    string `json:"filter,omitempty"`
}

// smartFilters are the Show filters a smart folder may name: the page's own.
var smartFilters = map[string]bool{"": true, "all": true, "prot": true, "dead": true, "moved": true, "moved-elsewhere": true,
	"unverified": true, "needs-login": true, "dupes": true}

// Defaults are what a new store reads.
func Defaults() Prefs {
	return Prefs{OpenWith: []string{"System default"}, Autosave: true, SaveMode: "change", IntervalSeconds: 300,
		WatchSeconds: 15, KeepDays: 14, RecheckDays: 7, SnapshotWidth: 1280, SnapshotHeight: 800, SnapshotWaitSeconds: 8,
		RenderChecks: true}
}

// Validate refuses values that would make the server misbehave (a 1-second poll, a negative age).
func (p Prefs) Validate() error {
	switch {
	case len(p.OpenWith) == 0 || len(p.OpenWith) > 12:
		return errors.New("open_with: name 1 to 12 browsers")
	case len(p.TabsOwn) > 500 || len(p.ActiveWorkspace) > 100:
		return errors.New("tabs_own: at most 500 workspaces")
	case len(p.Tracked) > 20:
		return errors.New("tracked: at most 20 browsers")
	case p.SaveMode != "change" && p.SaveMode != "interval":
		return errors.New(`save_mode: "change" or "interval"`)
	case p.IntervalSeconds < 60 || p.IntervalSeconds > 86400:
		return errors.New("interval_seconds: 60 to 86400")
	case p.WatchSeconds < 5 || p.WatchSeconds > 3600:
		return errors.New("watch_seconds: 5 to 3600")
	case p.KeepDays < 1 || p.KeepDays > 3650:
		return errors.New("keep_days: 1 to 3650")
	case p.RecheckDays < 0 || p.RecheckDays > 365:
		return errors.New("recheck_days: 0 (off) to 365")
	case p.IncognitoPurgeHours < 0 || p.IncognitoPurgeHours > 24*365:
		return errors.New("incognito_purge_hours: 0 (never) to 8760")
	case p.SnapshotWidth < 640 || p.SnapshotWidth > 2560:
		return errors.New("snapshot_width: 640 to 2560")
	case p.SnapshotHeight < 400 || p.SnapshotHeight > 4800:
		return errors.New("snapshot_height: 400 to 4800")
	case p.SnapshotWaitSeconds < 1 || p.SnapshotWaitSeconds > 30:
		return errors.New("snapshot_wait_seconds: 1 to 30")
	case p.SnapshotDir != "" && (!filepath.IsAbs(p.SnapshotDir) || len(p.SnapshotDir) > 1024 || filepath.Clean(p.SnapshotDir) != p.SnapshotDir):
		return errors.New("snapshot_dir: a full path to a folder (or empty for the default)")
	}
	if len(p.SmartFolders) > 50 {
		return errors.New("smart_folders: at most 50")
	}
	ids := map[string]bool{}
	for _, s := range p.SmartFolders {
		switch {
		case s.ID == "" || len(s.ID) > 40 || ids[s.ID]:
			return fmt.Errorf("smart folder %q: needs an id of its own, up to 40 characters", s.Name)
		case s.Name == "" || len(s.Name) > 60:
			return fmt.Errorf("smart folder %q: a name of 1 to 60 characters", s.Name)
		case len(s.Q) > 200 || len(s.Workspace) > 100:
			return fmt.Errorf("smart folder %q: its search is at most 200 characters", s.Name)
		case !smartFilters[s.Filter]:
			return fmt.Errorf("smart folder %q: filter %q is not one of the Show filters", s.Name, s.Filter)
		}
		ids[s.ID] = true
	}
	for _, n := range append(append([]string{}, p.OpenWith...), p.Tracked...) {
		if n == "" || len(n) > 60 {
			return fmt.Errorf("browser name %q: 1 to 60 characters", n)
		}
	}
	return nil
}

// PrefsStore is what a driver keeps: the preferences as one JSON document.
type PrefsStore interface {
	PrefsDoc(ctx context.Context) ([]byte, error) // nil when never saved
	SavePrefsDoc(ctx context.Context, doc []byte) error
}

// LoadPrefs reads the preferences, filling anything never saved from Defaults.
func LoadPrefs(ctx context.Context, s Store) (Prefs, error) {
	p := Defaults()
	doc, err := s.PrefsDoc(ctx)
	if err != nil || doc == nil {
		return p, err
	}
	if err := json.Unmarshal(doc, &p); err != nil {
		return Defaults(), fmt.Errorf("stored preferences are unreadable: %w", err)
	}
	return p, nil
}

// prefsMu serialises UpdatePrefs: the preferences are one document, so two read-change-save rounds running at once
// would each save over the other's change. It guards the stored document (one per process), never request data.
var prefsMu sync.Mutex

// UpdatePrefs reads the preferences, lets change edit them, and saves the result, as one step: concurrent updates
// (switching workspace while changing a setting, say) each see the other's change. A change returning an error saves
// nothing. It returns what was saved.
func UpdatePrefs(ctx context.Context, s Store, change func(*Prefs) error) (Prefs, error) {
	prefsMu.Lock()
	defer prefsMu.Unlock()
	p, err := LoadPrefs(ctx, s)
	if err != nil {
		return p, err
	}
	if err := change(&p); err != nil {
		return p, err
	}
	return p, SavePrefs(ctx, s, p)
}

// SavePrefs validates and stores the preferences. Prefer UpdatePrefs for a change to what is stored.
func SavePrefs(ctx context.Context, s Store, p Prefs) error {
	if err := p.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.SavePrefsDoc(ctx, b)
}

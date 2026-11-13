// Package tabs reads the open windows and tabs of the browsers on this computer, keeps snapshots of them, and puts
// a session back after a crash. Private (incognito) windows keep only their tab count unless the owner chose
// otherwise. Reading goes through a Reader, so tests never touch a real browser.
package tabs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// ID is a browser's own id for a window or tab. Browsers answer it as a number or as a string (Chrome does both,
// depending on its version), so it is kept as text either way.
type ID string

func (i *ID) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*i = ID(s)
		return nil
	}
	if string(b) == "null" {
		*i = ""
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*i = ID(n.String())
	return nil
}

// Tab is one open tab. ID is the browser's own id for it (Chromium browsers), "" when it has none.
type Tab struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	ID     ID     `json:"id,omitempty"`
	Pinned bool   `json:"pinned,omitempty"`
	Group  ID     `json:"group,omitempty"` // the tab group it is in (extensions only)
}

// Group is a tab group (Chromium; read through the extension).
type Group struct {
	ID        ID     `json:"id"`
	Title     string `json:"title"`
	Color     string `json:"color,omitempty"`
	Collapsed bool   `json:"collapsed,omitempty"`
}

// Window is one browser window: Mode is "normal" or "incognito". A private window that was redacted has TabCount and
// no Tabs.
type Window struct {
	Index    int     `json:"index"`
	ID       ID      `json:"id,omitempty"`
	Mode     string  `json:"mode"`
	Active   int     `json:"active,omitempty"`
	Tabs     []Tab   `json:"tabs,omitempty"`
	Groups   []Group `json:"groups,omitempty"`
	TabCount int     `json:"tab_count,omitempty"`
	Redacted bool    `json:"redacted,omitempty"`
}

// Browser is what one browser answered: its windows when it is running, or why it could not be read.
type Browser struct {
	Browser string   `json:"browser"`
	Running bool     `json:"running"`
	Windows []Window `json:"windows,omitempty"`
	Error   string   `json:"error,omitempty"` // "needs-permission", or what went wrong
	Hint    string   `json:"hint,omitempty"`
	// Via says how it was read: "extension" when its extension sent it, else through automation.
	Via string `json:"via,omitempty"`
}

// Summary counts windows and tabs, normal and private apart.
type Summary struct {
	Windows          int `json:"windows"`
	Tabs             int `json:"tabs"`
	IncognitoWindows int `json:"incognito_windows"`
	IncognitoTabs    int `json:"incognito_tabs"`
	Running          int `json:"running"`
}

// State is every tracked browser at one moment.
type State struct {
	At       time.Time `json:"at"`
	Browsers []Browser `json:"browsers"`
	Summary  Summary   `json:"summary"`
}

// Reader reads one browser. It must never start a browser that is not running.
type Reader interface {
	Read(ctx context.Context, b browsers.Browser) Browser
}

// ReadAll reads the given browsers at the same time and keeps their order.
func ReadAll(ctx context.Context, r Reader, bs []browsers.Browser, now time.Time) State {
	out := make([]Browser, len(bs))
	var wg sync.WaitGroup
	for i, b := range bs {
		wg.Add(1)
		go func() { defer wg.Done(); out[i] = r.Read(ctx, b) }()
	}
	wg.Wait()
	s := State{At: now.UTC(), Browsers: out}
	s.Summary = Summarise(out)
	return s
}

func Summarise(bs []Browser) Summary {
	var s Summary
	for _, b := range bs {
		if b.Running {
			s.Running++
		}
		for _, w := range b.Windows {
			n := len(w.Tabs)
			if w.Redacted {
				n = w.TabCount
			}
			if w.Mode == "incognito" {
				s.IncognitoWindows++
				s.IncognitoTabs += n
			} else {
				s.Windows++
				s.Tabs += n
			}
		}
	}
	return s
}

// Redact keeps private windows' tab counts and drops their pages, unless keep is set. It returns a copy.
func Redact(s State, keep bool) State {
	b, _ := json.Marshal(s)
	var out State
	_ = json.Unmarshal(b, &out)
	if keep {
		return out
	}
	for i := range out.Browsers {
		for j := range out.Browsers[i].Windows {
			w := &out.Browsers[i].Windows[j]
			if w.Mode == "incognito" && !w.Redacted {
				w.TabCount, w.Tabs, w.Groups, w.Redacted = len(w.Tabs), nil, nil, true
			}
		}
	}
	return out
}

// Fingerprint is what decides whether a snapshot is new: which browser, window and mode hold which addresses, in
// order. Titles and the time are left out, so a page that only changed its title is not a new session.
func Fingerprint(s State) string {
	type win struct {
		B, Mode string
		Index   int
		URLs    []string
		Count   int
	}
	var ws []win
	for _, b := range s.Browsers {
		for _, w := range b.Windows {
			x := win{B: b.Browser, Mode: w.Mode, Index: w.Index, Count: w.TabCount}
			for _, t := range w.Tabs {
				x.URLs = append(x.URLs, t.URL)
			}
			ws = append(ws, x)
		}
	}
	b, _ := json.Marshal(ws)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Readable reports whether any browser answered with windows.
func Readable(s State) bool {
	for _, b := range s.Browsers {
		if len(b.Windows) > 0 {
			return true
		}
	}
	return false
}

// RestoreWindow is one window to open again: its browser, whether private, and its pages.
type RestoreWindow struct {
	Browser string   `json:"browser"`
	Private bool     `json:"private"`
	URLs    []string `json:"urls"`
}

// RestorePlan lists the windows a snapshot can bring back (redacted ones cannot), only http(s) pages, at most
// browsers.MaxLinks each. pick limits it to those windows (by position in the snapshot); nil takes all.
func RestorePlan(s State, pick []int) []RestoreWindow {
	want := map[int]bool{}
	for _, p := range pick {
		want[p] = true
	}
	var out []RestoreWindow
	n := 0
	for _, b := range s.Browsers {
		for _, w := range b.Windows {
			k := n
			n++
			if (len(want) > 0 && !want[k]) || w.Redacted {
				continue
			}
			rw := RestoreWindow{Browser: b.Browser, Private: w.Mode == "incognito"}
			for _, t := range w.Tabs {
				if browsers.CheckLinks([]string{t.URL}) == nil && len(rw.URLs) < browsers.MaxLinks {
					rw.URLs = append(rw.URLs, t.URL)
				}
			}
			if len(rw.URLs) > 0 {
				out = append(out, rw)
			}
		}
	}
	return out
}

// CloseItem names a tab to close: by the browser's window and tab ids (Chromium) or by position (Safari), and the
// address it must still show, so a tab that moved on since the read is left alone.
type CloseItem struct {
	Browser  string `json:"browser"`
	WindowID ID     `json:"window_id"`
	TabID    ID     `json:"tab_id,omitempty"`
	Pos      int    `json:"pos"`
	URL      string `json:"url"`
}

// Closer closes tabs in one browser; it returns how many closed and why the others did not.
type Closer interface {
	Close(ctx context.Context, browser string, items []CloseItem) (int, []string, error)
}

// ErrNotScriptable: this browser's tabs cannot be read or closed.
var ErrNotScriptable = errors.New("this browser does not let other programs read or close its tabs")

// SortedNames lists the browsers in items, in a fixed order.
func SortedNames(items []CloseItem) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range items {
		if !seen[it.Browser] {
			seen[it.Browser] = true
			out = append(out, it.Browser)
		}
	}
	sort.Strings(out)
	return out
}

// ParseRead reads what the read script answered for one browser. A browser that cannot tell private windows apart
// has every window reported as normal.
func ParseRead(name string, privateKnown bool, stdout []byte) Browser {
	out := Browser{Browser: name}
	var doc struct {
		Running bool     `json:"running"`
		Windows []Window `json:"windows"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		out.Error = "unreadable answer: " + err.Error()
		return out
	}
	out.Running, out.Windows = doc.Running, doc.Windows
	for i := range out.Windows {
		if !privateKnown || out.Windows[i].Mode == "" {
			out.Windows[i].Mode = "normal"
		}
	}
	return out
}

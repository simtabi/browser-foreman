package tabs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// Sessions reads open tabs from the files browsers keep to restore a session: Firefox's recovery.jsonlz4 and
// Chromium's Sessions/Session_* command log. It works where automation does not (Firefox anywhere, every browser on
// Linux and Windows). Browsers leave private windows out of these files, so they are never read. What it reads is
// what the browser last wrote, a few seconds behind at most.
type Sessions struct {
	// Dirs returns the profile folders of a browser: Chromium ones hold Sessions/, Firefox ones sessionstore-backups/.
	Dirs func(b browsers.Browser) []string
	Now  func() time.Time
}

func (s Sessions) Read(_ context.Context, b browsers.Browser) Browser {
	out := Browser{Browser: b.Name, Via: "session file"}
	dirs := s.Dirs(b)
	if len(dirs) == 0 {
		out.Error = "no profile folder was found for it"
		return out
	}
	var newest time.Time
	for _, d := range dirs {
		var ws []Window
		var at time.Time
		var err error
		if b.Engine == "gecko" {
			ws, at, err = readFirefoxSession(d)
		} else {
			ws, at, err = readChromiumSession(d)
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			out.Error = err.Error()
			continue
		}
		out.Windows = append(out.Windows, ws...)
		if at.After(newest) {
			newest = at
		}
	}
	for i := range out.Windows {
		out.Windows[i].Index = i + 1
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	// A session file a running browser keeps writing is recent; an old one is what was open when it last quit.
	out.Running = !newest.IsZero() && now().Sub(newest) < 10*time.Minute
	if !out.Running {
		out.Windows = nil
		if out.Error == "" {
			out.Hint = "not running (its session file has not changed for a while)"
		}
	}
	return out
}

// ---- Firefox ----

func readFirefoxSession(profile string) ([]Window, time.Time, error) {
	var path string
	for _, p := range []string{"sessionstore-backups/recovery.jsonlz4", "sessionstore.jsonlz4"} {
		if _, err := os.Stat(filepath.Join(profile, p)); err == nil {
			path = filepath.Join(profile, p)
			break
		}
	}
	if path == "" {
		return nil, time.Time{}, os.ErrNotExist
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	b, err := mozLz4(raw)
	if err != nil {
		return nil, time.Time{}, err
	}
	var doc struct {
		Windows []struct {
			Selected  int  `json:"selected"`
			IsPrivate bool `json:"isPrivate"`
			Tabs      []struct {
				Index   int  `json:"index"`
				Pinned  bool `json:"pinned"`
				Hidden  bool `json:"hidden"`
				Entries []struct {
					URL   string `json:"url"`
					Title string `json:"title"`
				} `json:"entries"`
			} `json:"tabs"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, time.Time{}, errors.New("Firefox's session file is unreadable")
	}
	var out []Window
	for _, w := range doc.Windows {
		if w.IsPrivate {
			continue
		}
		win := Window{Mode: "normal", Active: w.Selected}
		for _, t := range w.Tabs {
			if t.Hidden || len(t.Entries) == 0 {
				continue
			}
			i := t.Index - 1
			if i < 0 || i >= len(t.Entries) {
				i = len(t.Entries) - 1
			}
			e := t.Entries[i]
			win.Tabs = append(win.Tabs, Tab{URL: e.URL, Title: e.Title, Pinned: t.Pinned})
		}
		if len(win.Tabs) > 0 {
			out = append(out, win)
		}
	}
	return out, st.ModTime(), nil
}

// mozLz4 opens Firefox's "mozLz40\0" files: a size, then one LZ4 block.
func mozLz4(b []byte) ([]byte, error) {
	if len(b) < 12 || !bytes.HasPrefix(b, []byte("mozLz40\x00")) {
		return nil, errors.New("not a mozLz4 file")
	}
	size := binary.LittleEndian.Uint32(b[8:12])
	if size > 256<<20 {
		return nil, errors.New("the session file is too large")
	}
	return lz4Block(b[12:], int(size))
}

// lz4Block decodes one LZ4 block (the format's sequences of literals and matches) into exactly size bytes.
func lz4Block(src []byte, size int) ([]byte, error) {
	bad := errors.New("the session file is damaged")
	dst := make([]byte, 0, size)
	for i := 0; i < len(src); {
		tok := src[i]
		i++
		n := int(tok >> 4)
		if n == 15 {
			for {
				if i >= len(src) {
					return nil, bad
				}
				c := src[i]
				i++
				n += int(c)
				if c != 255 {
					break
				}
			}
		}
		if i+n > len(src) || len(dst)+n > size {
			return nil, bad
		}
		dst = append(dst, src[i:i+n]...)
		i += n
		if i >= len(src) {
			break // the last sequence has literals only
		}
		if i+2 > len(src) {
			return nil, bad
		}
		off := int(src[i]) | int(src[i+1])<<8
		i += 2
		m := int(tok&15) + 4
		if tok&15 == 15 {
			for {
				if i >= len(src) {
					return nil, bad
				}
				c := src[i]
				i++
				m += int(c)
				if c != 255 {
					break
				}
			}
		}
		if off == 0 || off > len(dst) || len(dst)+m > size {
			return nil, bad
		}
		start := len(dst) - off
		for k := 0; k < m; k++ { // byte by byte: a match may overlap what it is copying
			dst = append(dst, dst[start+k])
		}
	}
	if len(dst) != size {
		return nil, bad
	}
	return dst, nil
}

// ---- Chromium ----

// Chromium's session commands (components/sessions/core/session_service_commands.cc).
const (
	cmdSetTabWindow          = 0
	cmdSetTabIndexInWindow   = 2
	cmdUpdateTabNavigation   = 6
	cmdSetSelectedNavIndex   = 7
	cmdSetSelectedTabInIndex = 8
	cmdSetPinnedState        = 12
	cmdTabClosed             = 16
	cmdWindowClosed          = 17
)

func newestSessionFile(profile string) (string, time.Time, error) {
	var best string
	var at time.Time
	cands, _ := filepath.Glob(filepath.Join(profile, "Sessions", "Session_*"))
	cands = append(cands, filepath.Join(profile, "Current Session"))
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && st.ModTime().After(at) {
			best, at = c, st.ModTime()
		}
	}
	if best == "" {
		return "", at, os.ErrNotExist
	}
	return best, at, nil
}

func readChromiumSession(profile string) ([]Window, time.Time, error) {
	path, at, err := newestSessionFile(profile)
	if err != nil {
		return nil, at, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, at, err
	}
	ws, err := parseSNSS(b)
	return ws, at, err
}

type snssTab struct {
	window, index, selected int32
	pinned, closed          bool
	navs                    map[int32]Tab
}

// parseSNSS replays a Chromium session file's commands into its windows and tabs.
func parseSNSS(b []byte) ([]Window, error) {
	if len(b) < 8 || string(b[:4]) != "SNSS" {
		return nil, errors.New("not a Chromium session file")
	}
	tabs := map[int32]*snssTab{}
	tab := func(id int32) *snssTab {
		if tabs[id] == nil {
			tabs[id] = &snssTab{window: -1, selected: -1, navs: map[int32]Tab{}}
		}
		return tabs[id]
	}
	closedWin := map[int32]bool{}
	activeTab := map[int32]int32{}
	i32 := func(p []byte, at int) int32 {
		if at+4 > len(p) {
			return 0
		}
		return int32(binary.LittleEndian.Uint32(p[at:]))
	}
	for i := 8; i+2 <= len(b); {
		n := int(binary.LittleEndian.Uint16(b[i:]))
		i += 2
		if n == 0 || i+n > len(b) {
			break // a command cut short: the browser was writing it
		}
		id, p := b[i], b[i+1:i+n]
		i += n
		switch id {
		case cmdSetTabWindow:
			tab(i32(p, 4)).window = i32(p, 0)
		case cmdSetTabIndexInWindow:
			tab(i32(p, 0)).index = i32(p, 4)
		case cmdSetSelectedNavIndex:
			tab(i32(p, 0)).selected = i32(p, 4)
		case cmdSetSelectedTabInIndex:
			activeTab[i32(p, 0)] = i32(p, 4)
		case cmdSetPinnedState:
			if len(p) >= 5 {
				tab(i32(p, 0)).pinned = p[4] != 0
			}
		case cmdTabClosed:
			tab(i32(p, 0)).closed = true
		case cmdWindowClosed:
			closedWin[i32(p, 0)] = true
		case cmdUpdateTabNavigation:
			// A pickle: a 4-byte payload size, the tab id, the navigation index, the address (UTF-8) and the title
			// (UTF-16), each string a 4-byte length and padded to 4 bytes.
			if len(p) < 12 {
				continue
			}
			r := pickle{b: p, at: 4}
			tid, nav := r.int32(), r.int32()
			u, t := r.str(), r.str16()
			if r.bad || u == "" {
				continue
			}
			tab(tid).navs[nav] = Tab{URL: u, Title: t}
		}
	}
	byWin := map[int32][]*snssTab{}
	for _, t := range tabs {
		if t.closed || t.window < 0 || closedWin[t.window] || len(t.navs) == 0 {
			continue
		}
		byWin[t.window] = append(byWin[t.window], t)
	}
	var ids []int32
	for w := range byWin {
		ids = append(ids, w)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var out []Window
	for _, w := range ids {
		ts := byWin[w]
		sort.SliceStable(ts, func(i, j int) bool { return ts[i].index < ts[j].index })
		win := Window{ID: ID(strconv.Itoa(int(w))), Mode: "normal"}
		for _, t := range ts {
			nav, ok := t.navs[t.selected]
			if !ok { // the newest navigation it has
				best := int32(-1)
				for k := range t.navs {
					if k > best {
						best = k
					}
				}
				nav = t.navs[best]
			}
			if strings.HasPrefix(nav.URL, "chrome://newtab") {
				nav.Title = "New Tab"
			}
			nav.Pinned = t.pinned
			win.Tabs = append(win.Tabs, nav)
		}
		if a, ok := activeTab[w]; ok && int(a) < len(win.Tabs) {
			win.Active = int(a) + 1
		}
		out = append(out, win)
	}
	return out, nil
}

type pickle struct {
	b   []byte
	at  int
	bad bool
}

func (p *pickle) int32() int32 {
	if p.at+4 > len(p.b) {
		p.bad = true
		return 0
	}
	v := int32(binary.LittleEndian.Uint32(p.b[p.at:]))
	p.at += 4
	return v
}

func pad4(n int) int { return (n + 3) &^ 3 }

func (p *pickle) str() string {
	n := int(p.int32())
	if p.bad || n < 0 || p.at+n > len(p.b) {
		p.bad = true
		return ""
	}
	s := string(p.b[p.at : p.at+n])
	p.at += pad4(n)
	return s
}

func (p *pickle) str16() string {
	n := int(p.int32())
	if p.bad || n < 0 || p.at+2*n > len(p.b) {
		p.bad = true
		return ""
	}
	u := make([]uint16, n)
	for k := range u {
		u[k] = binary.LittleEndian.Uint16(p.b[p.at+2*k:])
	}
	p.at += pad4(2 * n)
	return string(utf16.Decode(u))
}

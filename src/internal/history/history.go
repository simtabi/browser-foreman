// Package history reads browsers' visit history: which files there are and whether this program may read them (on
// macOS, Full Disk Access), and the pages visited, searched. Each database is read through a private copy, so a
// running browser is never disturbed and its locks never matter. Nothing read here is stored: history stays the
// browser's, and is only ever answered to the owner who asked.
package history

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Kinds of history file.
const (
	Chromium = "chromium"
	Firefox  = "firefox"
	Safari   = "safari"
)

// File is one profile's history database.
type File struct {
	Browser string `json:"browser"`
	Profile string `json:"profile,omitempty"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	State   string `json:"state"` // readable, blocked (the system refuses), missing
}

type place struct{ browser, kind, glob string }

// Find lists every profile's history file this program knows about, and whether each can be read. A browser with
// no profile at all is listed once, as missing; one whose folder the system closes is listed as blocked.
func Find() []File { return find(places()) }

func find(ps []place) []File {
	out := []File{}
	for _, p := range ps {
		matches, _ := filepath.Glob(p.glob)
		sort.Strings(matches)
		for _, m := range matches {
			out = append(out, File{Browser: p.browser, Profile: profileOf(p, m), Kind: p.kind, Path: m, State: probe(m)})
		}
		if len(matches) > 0 {
			continue
		}
		dir := p.glob
		if i := strings.Index(dir, "*"); i >= 0 {
			dir = dir[:i] + "x"
		}
		dir = filepath.Dir(dir)
		state := "missing"
		if _, err := os.ReadDir(dir); errors.Is(err, fs.ErrPermission) {
			state = "blocked"
		} else if _, err := os.Stat(p.glob); errors.Is(err, fs.ErrPermission) {
			state = "blocked"
		}
		out = append(out, File{Browser: p.browser, Kind: p.kind, Path: p.glob, State: state})
	}
	return out
}

func profileOf(p place, path string) string {
	if p.kind == Safari {
		return ""
	}
	return filepath.Base(filepath.Dir(path))
}

// probe reads one byte, never the contents.
func probe(path string) string {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "blocked"
	}
	defer f.Close()
	if _, err := f.Read(make([]byte, 1)); err != nil && !errors.Is(err, io.EOF) {
		return "blocked"
	}
	return "readable"
}

// Visit is one page in the history: the latest visit to it and how many there were.
type Visit struct {
	URL     string    `json:"url"`
	Title   string    `json:"title"`
	Last    time.Time `json:"last"`
	Visits  int       `json:"visits"`
	Browser string    `json:"browser"`
	Profile string    `json:"profile,omitempty"`
}

// Query narrows a search.
type Query struct {
	Words []string  // all must appear in the address or the title (lower case)
	Since time.Time // visits on or after
	Until time.Time // visits on or before (zero: up to now)
	// Want, when set, keeps only the pages it accepts, and Limit then counts those alone (up to 100,000): used to
	// find when each bookmark was last opened.
	Want  func(url string) bool
	Limit int
}

// Read searches one history file, newest first. Only http(s) pages are answered.
func Read(ctx context.Context, f File, q Query) ([]Visit, error) {
	dir, err := os.MkdirTemp("", "brofo-history-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	db := filepath.Join(dir, "h.sqlite")
	if err := copyFile(f.Path, db); err != nil {
		return nil, err
	}
	for _, side := range []string{"-wal"} {
		if err := copyFile(f.Path+side, db+side); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	conn, err := sql.Open("sqlite", "file:"+db)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_, _ = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	limit := q.Limit
	switch {
	case q.Want != nil && limit > 0 && limit <= 100_000:
	case limit <= 0 || limit > 5000:
		limit = 500
	}
	var query string
	var since, until int64 // the range in the file's own clock; the database skips what lies outside it
	switch f.Kind {
	case Chromium: // microseconds since 1601-01-01
		query = `SELECT url, COALESCE(title, ''), last_visit_time, visit_count FROM urls WHERE last_visit_time >= ? AND last_visit_time <= ? ORDER BY last_visit_time DESC`
		since = (q.Since.Unix() + 11644473600) * 1_000_000
		until = (q.Until.Unix()+11644473600)*1_000_000 + 999_999 // the whole second: Last is read to the second
	case Firefox: // microseconds since 1970
		query = `SELECT url, COALESCE(title, ''), COALESCE(last_visit_date, 0), visit_count FROM moz_places WHERE COALESCE(last_visit_date, 0) >= ? AND COALESCE(last_visit_date, 0) <= ? AND visit_count > 0 ORDER BY last_visit_date DESC`
		since = q.Since.UnixMicro()
		until = q.Until.UnixMicro()
	case Safari: // seconds since 2001-01-01, as a real
		query = `SELECT i.url, COALESCE((SELECT v2.title FROM history_visits v2 WHERE v2.history_item = i.id AND v2.title IS NOT NULL ORDER BY v2.visit_time DESC LIMIT 1), ''),
			CAST(MAX(v.visit_time) AS INTEGER), i.visit_count FROM history_items i JOIN history_visits v ON v.history_item = i.id
			GROUP BY i.id HAVING MAX(v.visit_time) >= ? AND MAX(v.visit_time) <= ? ORDER BY MAX(v.visit_time) DESC`
		since = q.Since.Unix() - 978307200
		until = q.Until.Unix() - 978307200 + 1 // MAX is a real: the whole second
	default:
		return nil, errors.New("unknown history kind " + f.Kind)
	}
	if q.Since.IsZero() {
		since = -1 << 62
	}
	if q.Until.IsZero() {
		until = 1 << 62
	}
	rows, err := conn.QueryContext(ctx, query, since, until)
	if err != nil {
		return nil, errors.New("not a " + f.Kind + " history database: " + err.Error())
	}
	defer rows.Close()
	out := []Visit{}
	for rows.Next() && len(out) < limit {
		var v Visit
		var at int64
		if err := rows.Scan(&v.URL, &v.Title, &at, &v.Visits); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(v.URL, "https://") && !strings.HasPrefix(v.URL, "http://") {
			continue
		}
		if q.Want != nil && !q.Want(v.URL) {
			continue
		}
		hay := strings.ToLower(v.URL + " " + v.Title)
		ok := true
		for _, w := range q.Words {
			ok = ok && strings.Contains(hay, w)
		}
		if !ok {
			continue
		}
		switch f.Kind {
		case Chromium:
			v.Last = time.Unix(at/1_000_000-11644473600, 0)
		case Firefox:
			v.Last = time.UnixMicro(at)
		case Safari:
			v.Last = time.Unix(at+978307200, 0)
		}
		if !q.Until.IsZero() && v.Last.After(q.Until) {
			continue // newer than the range: rows come newest first, so the limit counts only pages inside it
		}
		v.Browser, v.Profile = f.Browser, f.Profile
		out = append(out, v)
	}
	return out, rows.Err()
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

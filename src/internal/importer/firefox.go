package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // Firefox keeps bookmarks in SQLite

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Firefox is Firefox's places.sqlite.
const Firefox Format = "firefox-places"

// firefoxRoots are Firefox's root folders by guid, in the order they become top-level folders here.
var firefoxRoots = []struct{ guid, name string }{
	{"toolbar_____", "Bookmarks Bar"}, {"menu________", "Bookmarks Menu"}, {"unfiled_____", "Other bookmarks"}, {"mobile______", "Mobile bookmarks"},
}

// sqliteMagic starts every SQLite database file.
var sqliteMagic = []byte("SQLite format 3\x00")

// ReadFirefoxBytes reads a places.sqlite sent as bytes, through a private copy on disk.
func ReadFirefoxBytes(b []byte) (store.Tree, error) {
	dir, err := os.MkdirTemp("", "brofo-places-")
	if err != nil {
		return store.Tree{}, err
	}
	defer os.RemoveAll(dir)
	db := filepath.Join(dir, "places.sqlite")
	if err := os.WriteFile(db, b, 0o600); err != nil {
		return store.Tree{}, err
	}
	return readPlaces(db)
}

// ReadFirefoxFile reads a profile's places.sqlite through a copy (with its write-ahead log, where Firefox keeps the
// latest changes), so the live database is never opened and a running Firefox is never disturbed.
func ReadFirefoxFile(path string) (store.Tree, error) {
	dir, err := os.MkdirTemp("", "brofo-places-")
	if err != nil {
		return store.Tree{}, err
	}
	defer os.RemoveAll(dir)
	db := filepath.Join(dir, "places.sqlite")
	if err := copyFile(path, db); err != nil {
		return store.Tree{}, err
	}
	if err := copyFile(path+"-wal", db+"-wal"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return store.Tree{}, err
	}
	return readPlaces(db)
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

func readPlaces(db string) (store.Tree, error) {
	conn, err := sql.Open("sqlite", "file:"+db)
	if err != nil {
		return store.Tree{}, err
	}
	defer conn.Close()
	ctx := context.Background()
	_, _ = conn.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)") // fold the copied log into the copy
	rows, err := conn.QueryContext(ctx, `SELECT b.id, b.parent, b.type, COALESCE(b.title, ''), COALESCE(b.dateAdded, 0), COALESCE(b.guid, ''), COALESCE(p.url, '')
		FROM moz_bookmarks b LEFT JOIN moz_places p ON b.fk = p.id ORDER BY b.parent, b.position`)
	if err != nil {
		return store.Tree{}, fmt.Errorf("not a Firefox places database: %w", err)
	}
	defer rows.Close()
	type row struct {
		id, parent, typ int64
		title, guid     string
		added           int64
		url             string
	}
	kids := map[int64][]row{}
	byGUID := map[string]row{}
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.parent, &r.typ, &r.title, &r.added, &r.guid, &r.url); err != nil {
			return store.Tree{}, err
		}
		kids[r.parent] = append(kids[r.parent], r)
		byGUID[r.guid] = r
	}
	if err := rows.Err(); err != nil {
		return store.Tree{}, err
	}
	var id ids
	var fill func(f *store.Node, parent int64, depth int)
	fill = func(f *store.Node, parent int64, depth int) {
		if depth > 60 {
			return
		}
		for _, r := range kids[parent] {
			switch {
			case r.typ == 2:
				sub := &store.Node{ID: id.next(), Kind: store.KindFolder, Title: clean(r.title), Added: r.added / 1_000_000}
				fill(sub, r.id, depth+1)
				f.Children = append(f.Children, sub)
			case r.typ == 1 && r.url != "" && !strings.HasPrefix(r.url, "place:"):
				f.Children = append(f.Children, &store.Node{ID: id.next(), Kind: store.KindBookmark, Title: clean(r.title), URL: r.url, Added: r.added / 1_000_000})
			}
		}
	}
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	for _, fr := range firefoxRoots {
		r, ok := byGUID[fr.guid]
		if !ok {
			continue
		}
		f := &store.Node{ID: id.next(), Kind: store.KindFolder, Title: fr.name}
		fill(f, r.id, 0)
		if fr.guid == "mobile______" && len(f.Children) == 0 {
			continue
		}
		root.Children = append(root.Children, f)
	}
	if len(root.Children) == 0 {
		return store.Tree{}, errors.New("not a Firefox places database: it has no bookmark roots")
	}
	t := store.Tree{Root: root}
	return t, t.Validate()
}

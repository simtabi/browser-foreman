// Package chromefile reads and rewrites a Chromium profile's Bookmarks file for writing back into it: the profile's
// own ids and titles as written, every field Chromium keeps on a node (guid, dates, meta info) carried over, and the
// checksum Chromium checks on load. The importer is for reading bookmarks in; this is for writing a profile's own
// file, which must stay the profile's.
package chromefile

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/simtabi/browser-foreman/src/internal/atomicfile"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf16"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// rootKeys are Chromium's three roots, in the order a profile's tree lists them: bar, other, mobile.
var rootKeys = []string{"bookmark_bar", "other", "synced"}

// Doc is a Bookmarks file as read: its top-level fields and every node by id.
type Doc struct {
	top   map[string]json.RawMessage
	roots map[string]map[string]any
	nodes map[string]map[string]any
	maxID int64
}

// Read parses a Bookmarks file. The tree's top folders are the three roots in order (mobile too, even when empty); ids
// and titles are the file's own.
func Read(b []byte) (*Doc, store.Tree, error) {
	d := &Doc{roots: map[string]map[string]any{}, nodes: map[string]map[string]any{}}
	if err := json.Unmarshal(b, &d.top); err != nil {
		return nil, store.Tree{}, fmt.Errorf("Bookmarks file: %w", err)
	}
	var roots map[string]map[string]any
	if err := json.Unmarshal(d.top["roots"], &roots); err != nil {
		return nil, store.Tree{}, fmt.Errorf("Bookmarks file roots: %w", err)
	}
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	for _, k := range rootKeys {
		r, ok := roots[k]
		if !ok {
			return nil, store.Tree{}, fmt.Errorf("Bookmarks file has no %s root", k)
		}
		d.roots[k] = r
		n, err := d.conv(r)
		if err != nil {
			return nil, store.Tree{}, err
		}
		root.Children = append(root.Children, n)
	}
	t := store.Tree{Root: root}
	return d, t, t.Validate()
}

func (d *Doc) conv(m map[string]any) (*store.Node, error) {
	id, _ := m["id"].(string)
	if id == "" {
		return nil, errors.New("Bookmarks file has a node with no id")
	}
	if _, dup := d.nodes[id]; dup {
		return nil, fmt.Errorf("Bookmarks file has id %s twice", id)
	}
	d.nodes[id] = m
	if v, err := strconv.ParseInt(id, 10, 64); err == nil && v > d.maxID {
		d.maxID = v
	}
	name, _ := m["name"].(string)
	if m["type"] == "url" {
		u, _ := m["url"].(string)
		return &store.Node{ID: id, Kind: store.KindBookmark, Title: name, URL: u}, nil
	}
	n := &store.Node{ID: id, Kind: store.KindFolder, Title: name}
	kids, _ := m["children"].([]any)
	for _, k := range kids {
		km, ok := k.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Bookmarks file folder %s has a child that is not a node", id)
		}
		c, err := d.conv(km)
		if err != nil {
			return nil, err
		}
		n.Children = append(n.Children, c)
	}
	return n, nil
}

// SyncOn says whether the profile syncs its bookmarks: Chromium keeps sync's state in the file while it does.
func (d *Doc) SyncOn() bool {
	var s string
	return json.Unmarshal(d.top["sync_metadata"], &s) == nil && s != ""
}

// Encode writes target (the tree a writeback plan built from this file) as the file's new content. Nodes that came
// from the file keep all their fields; new ones get the next ids, a fresh guid and now as their date. Mobile
// bookmarks, when the target leaves it out, stays as it was. The checksum is Chromium's.
func (d *Doc) Encode(target store.Tree, now time.Time, guid func() (string, error)) ([]byte, error) {
	if target.Root == nil || len(target.Root.Children) < 2 {
		return nil, errors.New("the target needs the bar and other roots")
	}
	next := d.maxID
	stamp := strconv.FormatInt((now.Unix()+11644473600)*1_000_000+int64(now.Nanosecond()/1000), 10)
	seen := map[string]bool{}
	var conv func(n *store.Node) (map[string]any, error)
	conv = func(n *store.Node) (map[string]any, error) {
		if seen[n.ID] {
			return nil, fmt.Errorf("node %s appears twice in the target", n.ID)
		}
		seen[n.ID] = true
		m, ok := d.nodes[n.ID]
		if ok {
			m = clone(m)
		} else {
			next++
			g, err := guid()
			if err != nil {
				return nil, err
			}
			m = map[string]any{"id": strconv.FormatInt(next, 10), "guid": g, "date_added": stamp, "date_last_used": "0"}
			if n.Kind == store.KindFolder {
				m["date_modified"] = "0"
			}
		}
		m["name"] = n.Title
		if n.Kind == store.KindBookmark {
			if m["type"] == "folder" {
				return nil, fmt.Errorf("node %s is a folder in the file and a bookmark in the target", n.ID)
			}
			m["type"], m["url"] = "url", n.URL
			delete(m, "children")
			return m, nil
		}
		if m["type"] == "url" {
			return nil, fmt.Errorf("node %s is a bookmark in the file and a folder in the target", n.ID)
		}
		m["type"] = "folder"
		delete(m, "url")
		kids := []any{}
		for _, c := range n.Children {
			cm, err := conv(c)
			if err != nil {
				return nil, err
			}
			kids = append(kids, cm)
		}
		m["children"] = kids
		return m, nil
	}
	roots := map[string]any{}
	for i, k := range rootKeys {
		if i >= len(target.Root.Children) {
			roots[k] = d.roots[k]
			continue
		}
		top := target.Root.Children[i]
		if top.ID != d.roots[k]["id"] {
			return nil, fmt.Errorf("the target's root %d is %s, not the file's %s root", i+1, top.ID, k)
		}
		m, err := conv(top)
		if err != nil {
			return nil, err
		}
		m["name"] = d.roots[k]["name"] // a root keeps the name the browser gave it
		roots[k] = m
	}
	sum, err := Checksum(roots)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for k, v := range d.top {
		out[k] = v
	}
	out["roots"], out["checksum"] = roots, sum
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "   ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func clone(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// Checksum is Chromium's bookmarks checksum: MD5 over each node in tree order (bar, other, mobile): its id, its title
// as UTF-16 (little-endian), then "url" and the address, or "folder" and then its children.
func Checksum(roots map[string]any) (string, error) {
	h := md5.New()
	var upd func(v any) error
	upd = func(v any) error {
		m, ok := v.(map[string]any)
		if !ok {
			return errors.New("a node that is not an object")
		}
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		h.Write([]byte(id))
		for _, u := range utf16.Encode([]rune(name)) {
			_ = binary.Write(h, binary.LittleEndian, u)
		}
		if m["type"] == "url" {
			u, _ := m["url"].(string)
			h.Write([]byte("url"))
			h.Write([]byte(u))
			return nil
		}
		h.Write([]byte("folder"))
		kids, _ := m["children"].([]any)
		for _, k := range kids {
			if err := upd(k); err != nil {
				return err
			}
		}
		return nil
	}
	for _, k := range rootKeys {
		r, ok := roots[k]
		if !ok {
			return "", fmt.Errorf("no %s root", k)
		}
		if err := upd(r); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Running says whether the browser that owns the profile folder is running, from the lock Chromium keeps in its user
// data folder (the profile folder's parent) while it runs. How that lock looks differs by system; see running.
func Running(profileDir string) bool {
	return running(filepath.Dir(profileDir))
}

// Replace writes data as the profile's Bookmarks file, after copying the file as it is to keep (a path beside it that
// Chromium never writes). The new content goes to a temporary file first and is renamed into place, so the file is
// never half written.
func Replace(path string, data []byte, keep string) error {
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(keep, old, 0o600); err != nil {
		return fmt.Errorf("keeping the file as it was: %w", err)
	}
	return atomicfile.Write(path, data, info.Mode().Perm())
}

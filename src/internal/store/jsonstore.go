package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/simtabi/browser-foreman/src/internal/atomicfile"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// JSONStore keeps everything as plain files under <dir>/json:
//
//	workspaces/<id>/workspace.json   name and creation time
//	workspaces/<id>/history.jsonl    one Entry per line, appended
//	snapshots/<sha256>.json          each distinct tree, once
//	tabs/<id>.json                   one tab snapshot per file
//
// Files are written to a temporary name and renamed, so a crash never leaves half a file; directories are 0700 and
// files 0600, because they hold your browsing.
type JSONStore struct {
	mu   sync.Mutex
	root string
}

func OpenJSON(dir string) (*JSONStore, error) {
	root := filepath.Join(dir, "json")
	for _, d := range []string{"workspaces", "snapshots", "tabs"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			return nil, err
		}
	}
	return &JSONStore{root: root}, nil
}

func (s *JSONStore) Close() error { return nil }

func (s *JSONStore) wsDir(id string) string { return filepath.Join(s.root, "workspaces", id) }

func writeAtomic(path string, b []byte) error { return atomicfile.Write(path, b, 0o600) }

type wsFile struct {
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	Notes    string    `json:"notes,omitempty"`
	Locked   bool      `json:"locked,omitempty"`
	Archived bool      `json:"archived,omitempty"`
	Deleted  time.Time `json:"deleted,omitzero"`
	Color    string    `json:"color,omitempty"`
	Icon     string    `json:"icon,omitempty"`
}

func (s *JSONStore) CreateWorkspace(_ context.Context, id, name string, at time.Time) (Workspace, error) {
	if err := ValidID(id); err != nil {
		return Workspace{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	made := true
	if err := os.Mkdir(s.wsDir(id), 0o700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Workspace{}, err
		}
		// A folder with no workspace.json is a create that failed halfway: it may be made again.
		if _, err := os.Stat(filepath.Join(s.wsDir(id), "workspace.json")); !errors.Is(err, os.ErrNotExist) {
			return Workspace{}, fmt.Errorf("workspace %q already exists", id)
		}
		made = false
	}
	b, _ := json.Marshal(wsFile{Name: name, Created: at.UTC()})
	if err := writeAtomic(filepath.Join(s.wsDir(id), "workspace.json"), b); err != nil {
		if made {
			_ = os.Remove(s.wsDir(id)) // the empty folder this call made, so a retry starts clean
		}
		return Workspace{}, err
	}
	return Workspace{ID: id, Name: name, Created: at.UTC()}, nil
}

func (s *JSONStore) Workspaces(ctx context.Context) ([]Workspace, error) {
	ents, err := os.ReadDir(filepath.Join(s.root, "workspaces"))
	if err != nil {
		return nil, err
	}
	out := []Workspace{}
	for _, e := range ents {
		if !e.IsDir() || ValidID(e.Name()) != nil {
			continue
		}
		w, err := s.Workspace(ctx, e.Name())
		if errors.Is(err, ErrNotFound) {
			continue // a folder with no workspace.json: a create that failed halfway, not a workspace
		}
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *JSONStore) Workspace(_ context.Context, id string) (Workspace, error) {
	if ValidID(id) != nil {
		return Workspace{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workspace(id)
}

func (s *JSONStore) workspace(id string) (Workspace, error) {
	b, err := os.ReadFile(filepath.Join(s.wsDir(id), "workspace.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	var f wsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return Workspace{}, err
	}
	h, err := s.history(id)
	if err != nil {
		return Workspace{}, err
	}
	w := Workspace{ID: id, Name: f.Name, Created: f.Created, Notes: f.Notes, Locked: f.Locked, Archived: f.Archived, Deleted: f.Deleted,
		Color: f.Color, Icon: f.Icon}
	if len(h) > 0 {
		w.Head = h[len(h)-1].Seq
	}
	return w, nil
}

func (s *JSONStore) UpdateWorkspace(_ context.Context, w Workspace) error {
	if ValidID(w.ID) != nil {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.workspace(w.ID)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(wsFile{Name: w.Name, Created: old.Created, Notes: w.Notes, Locked: w.Locked, Archived: w.Archived, Deleted: w.Deleted,
		Color: w.Color, Icon: w.Icon})
	return writeAtomic(filepath.Join(s.wsDir(w.ID), "workspace.json"), b)
}

// history reads a workspace's entries. A last line with no newline is a write that never finished (a crash or a
// full disk mid-append): it was never confirmed to anyone, so it is not an entry, and the next Commit trims it. Any
// other line that does not parse is real damage and is reported.
func (s *JSONStore) history(id string) ([]Entry, error) {
	b, err := os.ReadFile(filepath.Join(s.wsDir(id), "history.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if i := bytes.LastIndexByte(b, '\n'); i < len(b)-1 {
		b = b[:i+1] // drop the torn tail
	}
	var out []Entry
	for line := range bytes.SplitSeq(b, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("workspace %s history: %w", id, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// trimTornTail cuts a half-written last line off an append-only file, so the next append starts on a line of its
// own instead of joining the fragment into one line nothing can read. It answers the size the file now has.
func trimTornTail(f *os.File) (int64, error) {
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return 0, err
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		return 0, err
	}
	keep := int64(bytes.LastIndexByte(b, '\n') + 1)
	if keep == int64(len(b)) {
		return keep, nil
	}
	return keep, f.Truncate(keep)
}

func (s *JSONStore) History(_ context.Context, id string) ([]Entry, error) {
	if ValidID(id) != nil {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.wsDir(id)); errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return s.history(id)
}

func (s *JSONStore) Commit(_ context.Context, id string, base int, c Change, at time.Time) (Entry, error) {
	if err := c.Tree.Validate(); err != nil {
		return Entry{}, err
	}
	if ValidID(id) != nil {
		return Entry{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.workspace(id)
	if err != nil {
		return Entry{}, err
	}
	if w.Locked {
		return Entry{}, ErrLocked
	}
	if base != w.Head {
		return Entry{}, fmt.Errorf("%w (made on %d, now %d)", ErrConflict, base, w.Head)
	}
	hash, body, err := Hash(c.Tree)
	if err != nil {
		return Entry{}, err
	}
	snap := filepath.Join(s.root, "snapshots", hash+".json")
	if _, err := os.Stat(snap); errors.Is(err, os.ErrNotExist) {
		if err := writeAtomic(snap, body); err != nil {
			return Entry{}, err
		}
	}
	e := Entry{Seq: w.Head + 1, At: at.UTC(), Op: c.Op, Summary: c.Summary, Base: base, Snap: hash, Undid: c.Undid, Redone: c.Redone}
	line, _ := json.Marshal(e)
	f, err := os.OpenFile(filepath.Join(s.wsDir(id), "history.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return Entry{}, err
	}
	size, err := trimTornTail(f)
	if err != nil {
		f.Close()
		return Entry{}, err
	}
	_, err = f.Write(append(line, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		_ = f.Truncate(size) // take back a partial line, so this failure cannot damage the file
		f.Close()
		return Entry{}, err
	}
	return e, f.Close()
}

func (s *JSONStore) State(_ context.Context, id string, seq int) (Tree, Entry, error) {
	if ValidID(id) != nil {
		return Tree{}, Entry{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h, err := s.history(id)
	if err != nil {
		return Tree{}, Entry{}, err
	}
	if len(h) == 0 {
		return Tree{}, Entry{}, ErrNotFound
	}
	e := h[len(h)-1]
	if seq != 0 {
		if seq < 1 || seq > len(h) {
			return Tree{}, Entry{}, ErrNotFound
		}
		e = h[seq-1]
	}
	b, err := os.ReadFile(filepath.Join(s.root, "snapshots", e.Snap+".json"))
	if err != nil {
		return Tree{}, e, err
	}
	var t Tree
	return t, e, json.Unmarshal(b, &t)
}

func (s *JSONStore) SaveTabs(_ context.Context, t TabSnapshot) error {
	if err := ValidID(t.ID); err != nil {
		return err
	}
	t.Pool = PoolOf(t.Windows) // the document names its pool; the field only lists it
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeAtomic(filepath.Join(s.root, "tabs", t.ID+".json"), b)
}

func (s *JSONStore) TabSnapshots(_ context.Context, limit int) ([]TabSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := os.ReadDir(filepath.Join(s.root, "tabs"))
	if err != nil {
		return nil, err
	}
	out := []TabSnapshot{}
	for _, e := range ents {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.root, "tabs", e.Name()))
		if err != nil {
			return nil, err
		}
		var t TabSnapshot
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, err
		}
		if t.Pool == "" {
			t.Pool = PoolOf(t.Windows) // saved before Pool existed
		}
		t.Windows = nil
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *JSONStore) TabSnapshot(_ context.Context, id string) (TabSnapshot, error) {
	if ValidID(id) != nil {
		return TabSnapshot{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.root, "tabs", id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return TabSnapshot{}, ErrNotFound
	}
	if err != nil {
		return TabSnapshot{}, err
	}
	var t TabSnapshot
	return t, json.Unmarshal(b, &t)
}

func (s *JSONStore) PurgeWorkspace(_ context.Context, id string) error {
	if ValidID(id) != nil {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(filepath.Join(s.wsDir(id), "workspace.json")); err != nil {
		return ErrNotFound
	}
	if err := os.RemoveAll(s.wsDir(id)); err != nil {
		return err
	}
	// Trees are shared by content: remove only those no remaining workspace's history names.
	used := map[string]bool{}
	dirs, err := os.ReadDir(filepath.Join(s.root, "workspaces"))
	if err != nil {
		return err
	}
	for _, d := range dirs {
		h, err := s.history(d.Name())
		if err != nil {
			return err
		}
		for _, e := range h {
			used[e.Snap] = true
		}
	}
	snaps, err := os.ReadDir(filepath.Join(s.root, "snapshots"))
	if err != nil {
		return err
	}
	for _, f := range snaps {
		if hash, ok := strings.CutSuffix(f.Name(), ".json"); ok && !used[hash] {
			if err := os.Remove(filepath.Join(s.root, "snapshots", f.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

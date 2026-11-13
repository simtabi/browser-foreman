// Package store keeps Browser Foreman's data: workspaces (a bookmark tree with its full history) and tab snapshots.
//
// Two drivers implement Store and pass the same test suite: "sqlite" (default; one database file, pure Go) and "json"
// (plain files: easy to read, back up and sync). Open picks one from the configuration.
//
// History is append-only. Every change is an Entry holding the whole tree as it was afterwards, stored once per
// distinct content (snapshots are keyed by their SHA-256), so any past state reads back directly. Undo and redo are new
// entries too (see history.go), so they can be undone in turn and nothing is ever rewritten.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrConflict: the change was made on an older state than the workspace now has; reload and try again.
	ErrConflict = errors.New("the workspace changed since this change was made")
	// ErrLocked: the workspace is locked (read-only); every change is refused until it is unlocked.
	ErrLocked = errors.New("the workspace is locked (read-only); unlock it to change it")
)

// Node is a folder or a bookmark. A folder has Children; a bookmark has a URL.
type Node struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // "folder" or "bookmark"
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Note  string `json:"note,omitempty"`
	// Description is what the page itself says it is (its meta description), when known.
	Description string  `json:"description,omitempty"`
	Added       int64   `json:"added,omitempty"` // Unix seconds, when known
	Children    []*Node `json:"children,omitempty"`
}

const (
	KindFolder   = "folder"
	KindBookmark = "bookmark"
)

// Tree is a workspace's bookmarks: an unnamed root folder holding the browser roots (Bookmarks Bar, Other bookmarks…),
// and the Trash: what was removed, with where it came from, so it can go back. An empty Trash is left out of the JSON,
// so trees stored before the Trash existed keep their content hash.
type Tree struct {
	Root  *Node      `json:"root"`
	Trash []*Trashed `json:"trash,omitempty"`
}

// Trashed is a node in the Trash (a folder keeps its contents), with the folder and position it left.
type Trashed struct {
	Node   *Node  `json:"node"`
	From   string `json:"from"`  // the parent folder's id
	Index  int    `json:"index"` // its position there
	At     int64  `json:"at"`    // Unix seconds
	Reason string `json:"reason,omitempty"`
}

// Count returns how many bookmarks and folders the tree holds (the root not included).
func (t Tree) Count() (bookmarks, folders int) {
	var walk func(*Node)
	walk = func(n *Node) {
		for _, c := range n.Children {
			if c.Kind == KindFolder {
				folders++
				walk(c)
			} else {
				bookmarks++
			}
		}
	}
	if t.Root != nil {
		walk(t.Root)
	}
	return
}

// Validate refuses a tree a driver should not store: no root, unknown kinds, bookmarks with children, repeated ids.
func (t Tree) Validate() error {
	if t.Root == nil || t.Root.Kind != KindFolder {
		return errors.New("a tree needs a root folder")
	}
	seen := map[string]bool{}
	var walk func(*Node, int) error
	walk = func(n *Node, depth int) error {
		if depth > 64 {
			return errors.New("folders are nested more than 64 deep")
		}
		for _, c := range n.Children {
			if c == nil || c.ID == "" {
				return errors.New("every node needs an id")
			}
			if seen[c.ID] {
				return fmt.Errorf("node id %q appears twice", c.ID)
			}
			seen[c.ID] = true
			switch c.Kind {
			case KindFolder:
				if err := walk(c, depth+1); err != nil {
					return err
				}
			case KindBookmark:
				if len(c.Children) > 0 {
					return fmt.Errorf("bookmark %q has children", c.ID)
				}
			default:
				return fmt.Errorf("node %q has kind %q (folder or bookmark)", c.ID, c.Kind)
			}
		}
		return nil
	}
	if err := walk(t.Root, 0); err != nil {
		return err
	}
	for _, x := range t.Trash {
		if x == nil || x.Node == nil || x.Node.ID == "" {
			return errors.New("a trashed entry needs a node with an id")
		}
		if seen[x.Node.ID] {
			return fmt.Errorf("node id %q appears twice", x.Node.ID)
		}
		seen[x.Node.ID] = true
		if err := walk(&Node{Kind: KindFolder, Children: x.Node.Children}, 1); err != nil {
			return err
		}
	}
	return nil
}

// Hash is the content address of a tree: SHA-256 of its JSON.
func Hash(t Tree) (string, []byte, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), b, nil
}

// Workspace is one bookmark set with its history.
type Workspace struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
	Head    int       `json:"head"` // the last entry's sequence number; 0 before the first
	Notes   string    `json:"notes,omitempty"`
	// Locked: read-only; Commit refuses every change (ErrLocked) until it is unlocked.
	Locked bool `json:"locked,omitempty"`
	// Archived: kept, out of the way; lists show it last and the web interface does not open it by default.
	Archived bool `json:"archived,omitempty"`
	// Deleted: when it went to the workspace Trash (zero: it is not there). It keeps everything, history included,
	// until it is restored or the Trash is emptied (PurgeWorkspace).
	Deleted time.Time `json:"deleted,omitzero"`
}

// Entry is one step in a workspace's history.
type Entry struct {
	Seq     int       `json:"seq"`
	At      time.Time `json:"at"`
	Op      string    `json:"op"` // import, edit, undo, redo, migrate…
	Summary string    `json:"summary"`
	Base    int       `json:"base"`             // the head this was made on
	Snap    string    `json:"snap"`             // the tree afterwards, by content hash
	Undid   int       `json:"undid,omitempty"`  // undo: the entry it took back
	Redone  int       `json:"redone,omitempty"` // redo: the entry it put back
}

// Change is what a caller asks to record.
type Change struct {
	Op      string
	Summary string
	Tree    Tree
	Undid   int
	Redone  int
}

// TabSnapshot is the open windows and tabs at one moment, as the browsers reported them.
type TabSnapshot struct {
	ID      string          `json:"id"`
	At      time.Time       `json:"at"`
	Reason  string          `json:"reason"` // auto, manual, before-close…
	Windows json.RawMessage `json:"windows"`
}

// Store is what every driver provides. Implementations are safe for use by one process at a time.
type Store interface {
	CreateWorkspace(ctx context.Context, id, name string, at time.Time) (Workspace, error)
	Workspaces(ctx context.Context) ([]Workspace, error)
	Workspace(ctx context.Context, id string) (Workspace, error)
	// UpdateWorkspace sets a workspace's name, notes, lock and archive flag (not its history).
	UpdateWorkspace(ctx context.Context, w Workspace) error
	// Commit appends a change made on `base` (the head the caller saw). A base that is not the head is ErrConflict.
	Commit(ctx context.Context, wsID string, base int, c Change, at time.Time) (Entry, error)
	History(ctx context.Context, wsID string) ([]Entry, error)
	// PurgeWorkspace removes a workspace for good, with its history, sources and the trees no other workspace uses.
	PurgeWorkspace(ctx context.Context, wsID string) error
	// State reads the tree after entry seq (0: the head).
	State(ctx context.Context, wsID string, seq int) (Tree, Entry, error)

	SaveTabs(ctx context.Context, s TabSnapshot) error
	TabSnapshots(ctx context.Context, limit int) ([]TabSnapshot, error) // newest first, Windows left out
	TabSnapshot(ctx context.Context, id string) (TabSnapshot, error)

	Accounts
	Links
	PrefsStore
	Sources
	TabExtras

	Close() error
}

// ValidID accepts the ids this package uses in file names and keys: letters, digits, '-' and '_', 1–64 long.
func ValidID(id string) error {
	if id == "" || len(id) > 64 || strings.Trim(id, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
		return fmt.Errorf("id %q: use 1-64 letters, digits, - or _", id)
	}
	return nil
}

// Open returns the driver the configuration names, keeping its data under dir.
func Open(driver, dir string) (Store, error) {
	switch driver {
	case "json":
		return OpenJSON(dir)
	case "sqlite":
		return OpenSQLite(dir)
	}
	return nil, fmt.Errorf("storage driver %q: use sqlite or json", driver)
}

package store

import (
	"context"
	"time"
)

// Note kinds: a tab (keyed by its address), a window (by browser and window id) or a snapshot (by its id).
const (
	NoteTab      = "tab"
	NoteWindow   = "window"
	NoteSnapshot = "snapshot"
)

// TabNote is a note and tags on a tab, a window or a snapshot. Notes are kept apart from snapshots, so deleting or
// purging snapshots leaves them.
type TabNote struct {
	Kind    string    `json:"kind"`
	Key     string    `json:"key"`
	Note    string    `json:"note"`
	Tags    []string  `json:"tags"`
	Updated time.Time `json:"updated"`
}

// TabExtras is what a driver keeps beside tab snapshots.
type TabExtras interface {
	// SetTabNote saves a note; an empty note with no tags removes it.
	SetTabNote(ctx context.Context, n TabNote) error
	// TabNotes is every note, newest first.
	TabNotes(ctx context.Context) ([]TabNote, error)
	// DeleteTabs deletes snapshots for good and answers how many there were.
	DeleteTabs(ctx context.Context, ids []string) (int, error)
	// Scrub makes deleted and replaced snapshot data unreadable where the driver can: SQLite overwrites freed pages
	// (secure_delete) and folds its write-ahead log back in.
	Scrub(ctx context.Context) error
}

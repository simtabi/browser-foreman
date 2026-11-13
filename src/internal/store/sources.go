package store

import (
	"context"
	"time"
)

// Source kinds.
const (
	SourceFile      = "file"      // a file sent in; a copy is kept, so Update reads the same bytes again
	SourcePath      = "path"      // a bookmarks file on this computer (a browser profile); Update reads it again
	SourceWorkspace = "workspace" // another workspace; Update reads its current bookmarks
)

// Source is one place a workspace's bookmarks came from, kept so Update can read it again.
type Source struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Path     string    `json:"path,omitempty"` // SourcePath
	From     string    `json:"from,omitempty"` // SourceWorkspace: its id
	SHA256   string    `json:"sha256,omitempty"`
	Count    int       `json:"count"` // bookmarks it held when added
	Separate bool      `json:"separate,omitempty"`
	Added    time.Time `json:"added"`
}

// Sources is what a driver keeps about where workspaces' bookmarks came from.
type Sources interface {
	// AddSource records a source of a workspace, replacing one with the same id; body is the file's bytes for
	// SourceFile (nil otherwise).
	AddSource(ctx context.Context, wsID string, s Source, body []byte) error
	// SourceList is a workspace's sources, oldest first.
	SourceList(ctx context.Context, wsID string) ([]Source, error)
	// SourceBody is a SourceFile's kept bytes; ErrNotFound when there are none.
	SourceBody(ctx context.Context, wsID, id string) ([]byte, error)
}

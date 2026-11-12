package store

import (
	"context"
	"time"
)

// LinkResult is the latest check of one URL. Results are kept per address, not per bookmark, so every workspace
// holding that address shares them.
type LinkResult struct {
	URL     string    `json:"url"`
	Status  string    `json:"status"` // ok, moved, needs-login, unverified, dead, local, skipped
	Code    int       `json:"code,omitempty"`
	Final   string    `json:"final,omitempty"` // where it ended up, when that differs
	Note    string    `json:"note,omitempty"`
	Kind    string    `json:"kind,omitempty"` // for dead links: "page" or "host"
	Checked time.Time `json:"checked"`
}

// Links is what a driver keeps about link checks.
type Links interface {
	// SaveLinks records results, replacing what was known about each URL.
	SaveLinks(ctx context.Context, rs []LinkResult) error
	// Links returns what is known about urls (those never checked are left out); nil urls means every URL.
	Links(ctx context.Context, urls []string) (map[string]LinkResult, error)
}

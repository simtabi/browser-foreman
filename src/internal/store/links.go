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
	// DeadSince is when checks first found the link dead; Confirmed is set once a later check, at least a day after,
	// found it dead again. A link seen dead once is only a suspicion: sites have bad hours.
	DeadSince time.Time `json:"dead_since,omitzero"`
	Confirmed bool      `json:"confirmed,omitempty"`
	// Frame is "refused" when the page forbids being shown inside another page (X-Frame-Options or CSP
	// frame-ancestors), so a live preview would be blank.
	Frame string `json:"frame,omitempty"`
	// Meta is what the page says about itself (OpenGraph and the canonical link), from its first 64 KB.
	Meta *PageMeta `json:"meta,omitempty"`
	// Archive is the Internet Archive's closest copy of a confirmed-dead link, when the owner turned that lookup on.
	Archive string `json:"archive,omitempty"`
}

// PageMeta is a page's own description of itself.
type PageMeta struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	Canonical   string `json:"canonical,omitempty"`
	Site        string `json:"site,omitempty"`
}

// Links is what a driver keeps about link checks.
type Links interface {
	// SaveLinks records results, replacing what was known about each URL.
	SaveLinks(ctx context.Context, rs []LinkResult) error
	// Links returns what is known about urls (those never checked are left out); nil urls means every URL.
	Links(ctx context.Context, urls []string) (map[string]LinkResult, error)
}

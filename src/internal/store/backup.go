package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// BackupFormat names the file a backup is; Restore refuses anything else.
const BackupFormat = "browser-foreman-backup"

// Backup is everything worth keeping that is not a secret: every workspace with its whole history (each distinct
// tree once, by hash) and its notes and flags, and every link check result. People, API tokens, sessions and webhook
// secrets are left out on purpose, so a backup file can be stored or shared without handing over a way in.
type Backup struct {
	Format     string                `json:"format"`
	Version    int                   `json:"version"`
	Created    time.Time             `json:"created"`
	Workspaces []BackupWorkspace     `json:"workspaces"`
	Trees      map[string]Tree       `json:"trees"`
	Links      map[string]LinkResult `json:"links"`
	// Tabs are the tab snapshots, each with its windows and pages.
	Tabs []TabSnapshot `json:"tabs,omitempty"`
	// Prefs are the preferences document, as stored.
	Prefs json.RawMessage `json:"prefs,omitempty"`
}

type BackupWorkspace struct {
	Workspace Workspace `json:"workspace"`
	History   []Entry   `json:"history"`
	// Sources are where its bookmarks came from, each with its kept file, so Update works after a restore.
	Sources []BackupSource `json:"sources,omitempty"`
}

// BackupSource is a Source and, for a kept file, its bytes.
type BackupSource struct {
	Source Source `json:"source"`
	Body   []byte `json:"body,omitempty"`
}

// MakeBackup reads everything into one Backup.
func MakeBackup(ctx context.Context, s Store, now time.Time) (Backup, error) {
	b := Backup{Format: BackupFormat, Version: 1, Created: now.UTC(), Trees: map[string]Tree{}}
	all, err := s.Workspaces(ctx)
	if err != nil {
		return b, err
	}
	for _, w := range all {
		h, err := s.History(ctx, w.ID)
		if err != nil {
			return b, err
		}
		for _, e := range h {
			if _, ok := b.Trees[e.Snap]; ok {
				continue
			}
			t, _, err := s.State(ctx, w.ID, e.Seq)
			if err != nil {
				return b, err
			}
			b.Trees[e.Snap] = t
		}
		bw := BackupWorkspace{Workspace: w, History: h}
		list, err := s.SourceList(ctx, w.ID)
		if err != nil {
			return b, err
		}
		for _, x := range list {
			body, err := s.SourceBody(ctx, w.ID, x.ID)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return b, err
			}
			bw.Sources = append(bw.Sources, BackupSource{Source: x, Body: body})
		}
		b.Workspaces = append(b.Workspaces, bw)
	}
	if b.Links, err = s.Links(ctx, nil); err != nil {
		return b, err
	}
	snaps, err := s.TabSnapshots(ctx, 0)
	if err != nil {
		return b, err
	}
	for i := len(snaps) - 1; i >= 0; i-- { // oldest first
		full, err := s.TabSnapshot(ctx, snaps[i].ID)
		if err != nil {
			return b, err
		}
		b.Tabs = append(b.Tabs, full)
	}
	if b.Prefs, err = s.PrefsDoc(ctx); err != nil {
		return b, err
	}
	return b, nil
}

// Restored says what a restore did: each workspace's id in the backup and the id it has now.
type Restored struct {
	Workspaces map[string]string `json:"workspaces"`
	Entries    int               `json:"entries"`
	Links      int               `json:"links"`
	Tabs       int               `json:"tabs"`
	Prefs      bool              `json:"prefs"`
}

// Restore adds what a backup holds, replacing nothing: a workspace whose id is taken comes back under a new id
// (<id>-restored-<n>), with its whole history; link results are merged, the newer check of an address winning.
func Restore(ctx context.Context, s Store, b Backup, now time.Time) (Restored, error) {
	out := Restored{Workspaces: map[string]string{}}
	if b.Format != BackupFormat || b.Version != 1 {
		return out, errors.New("not a Browser Foreman backup (or one from a newer version)")
	}
	for _, bw := range b.Workspaces {
		for _, e := range bw.History {
			if _, ok := b.Trees[e.Snap]; !ok {
				return out, fmt.Errorf("the backup is incomplete: workspace %s entry %d has no tree", bw.Workspace.ID, e.Seq)
			}
		}
	}
	for _, bw := range b.Workspaces {
		w := bw.Workspace
		id := w.ID
		for n := 1; ; n++ {
			if _, err := s.Workspace(ctx, id); errors.Is(err, ErrNotFound) {
				break
			} else if err != nil {
				return out, err
			}
			id = fmt.Sprintf("%s-restored-%d", w.ID, n)
			if len(id) > 64 {
				id = fmt.Sprintf("restored-%d-%d", now.Unix(), n)
			}
		}
		if _, err := s.CreateWorkspace(ctx, id, w.Name, w.Created); err != nil {
			return out, err
		}
		for _, e := range bw.History {
			c := Change{Op: e.Op, Summary: e.Summary, Tree: b.Trees[e.Snap], Undid: e.Undid, Redone: e.Redone}
			if _, err := s.Commit(ctx, id, e.Seq-1, c, e.At); err != nil {
				return out, fmt.Errorf("workspace %s entry %d: %w", id, e.Seq, err)
			}
			out.Entries++
		}
		for _, x := range bw.Sources {
			if err := s.AddSource(ctx, id, x.Source, x.Body); err != nil {
				return out, fmt.Errorf("workspace %s source %s: %w", id, x.Source.ID, err)
			}
		}
		w.ID = id
		if err := s.UpdateWorkspace(ctx, w); err != nil { // notes, lock and archive last: a lock would refuse the entries
			return out, err
		}
		out.Workspaces[bw.Workspace.ID] = id
	}
	urls := make([]string, 0, len(b.Links))
	for u := range b.Links {
		urls = append(urls, u)
	}
	have, err := s.Links(ctx, urls)
	if err != nil {
		return out, err
	}
	var newer []LinkResult
	for u, r := range b.Links {
		if h, ok := have[u]; !ok || r.Checked.After(h.Checked) {
			newer = append(newer, r)
		}
	}
	out.Links = len(newer)
	if err := s.SaveLinks(ctx, newer); err != nil {
		return out, err
	}
	for _, t := range b.Tabs { // a snapshot already here (same id) is kept as it is
		if _, err := s.TabSnapshot(ctx, t.ID); err == nil {
			continue
		}
		if err := s.SaveTabs(ctx, t); err != nil {
			return out, fmt.Errorf("tab snapshot %s: %w", t.ID, err)
		}
		out.Tabs++
	}
	if len(b.Prefs) > 0 { // only into a store whose preferences were never set
		have, err := s.PrefsDoc(ctx)
		if err != nil {
			return out, err
		}
		if have == nil {
			p := Defaults()
			if err := json.Unmarshal(b.Prefs, &p); err != nil {
				return out, fmt.Errorf("the backup's preferences are unreadable: %w", err)
			}
			if err := SavePrefs(ctx, s, p); err != nil {
				return out, fmt.Errorf("the backup's preferences: %w", err)
			}
			out.Prefs = true
		}
	}
	return out, nil
}

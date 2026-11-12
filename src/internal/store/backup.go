package store

import (
	"context"
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
}

type BackupWorkspace struct {
	Workspace Workspace `json:"workspace"`
	History   []Entry   `json:"history"`
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
		b.Workspaces = append(b.Workspaces, BackupWorkspace{Workspace: w, History: h})
	}
	if b.Links, err = s.Links(ctx, nil); err != nil {
		return b, err
	}
	return b, nil
}

// Restored says what a restore did: each workspace's id in the backup and the id it has now.
type Restored struct {
	Workspaces map[string]string `json:"workspaces"`
	Entries    int               `json:"entries"`
	Links      int               `json:"links"`
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
	return out, s.SaveLinks(ctx, newer)
}

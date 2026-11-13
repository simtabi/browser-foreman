package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Stacks replays a history into what Undo and Redo would act on. Every change other than undo and redo pushes onto
// the undo stack and clears the redo stack; an undo moves its entry from undo to redo; a redo moves it back.
func Stacks(h []Entry) (undo, redo []int) {
	for _, e := range h {
		switch e.Op {
		case "undo":
			if n := len(undo); n > 0 {
				undo = undo[:n-1]
			}
			redo = append(redo, e.Undid)
		case "redo":
			if n := len(redo); n > 0 {
				redo = redo[:n-1]
			}
			undo = append(undo, e.Redone)
		default:
			undo = append(undo, e.Seq)
			redo = nil
		}
	}
	return undo, redo
}

var ErrNothing = errors.New("nothing to do")

// Undo takes back the last change: a new entry whose tree is the state before it. The first entry (the import that
// created the workspace) cannot be undone.
func Undo(ctx context.Context, s Store, wsID string, at time.Time) (Entry, error) {
	h, err := s.History(ctx, wsID)
	if err != nil {
		return Entry{}, err
	}
	undo, _ := Stacks(h)
	if len(undo) < 2 {
		return Entry{}, fmt.Errorf("%w: no change to undo", ErrNothing)
	}
	last, before := undo[len(undo)-1], undo[len(undo)-2]
	tree, _, err := s.State(ctx, wsID, before)
	if err != nil {
		return Entry{}, err
	}
	return s.Commit(ctx, wsID, h[len(h)-1].Seq, Change{Op: "undo", Summary: "Undid: " + h[last-1].Summary, Tree: tree, Undid: last}, at)
}

// Redo puts back the last change Undo took back, until a new change is made.
func Redo(ctx context.Context, s Store, wsID string, at time.Time) (Entry, error) {
	h, err := s.History(ctx, wsID)
	if err != nil {
		return Entry{}, err
	}
	_, redo := Stacks(h)
	if len(redo) == 0 {
		return Entry{}, fmt.Errorf("%w: no undone change to redo", ErrNothing)
	}
	target := redo[len(redo)-1]
	tree, _, err := s.State(ctx, wsID, target)
	if err != nil {
		return Entry{}, err
	}
	return s.Commit(ctx, wsID, h[len(h)-1].Seq, Change{Op: "redo", Summary: "Redid: " + h[target-1].Summary, Tree: tree, Redone: target}, at)
}

// Migrate copies every workspace (with its whole history, entry by entry, times and all), every user and API token,
// every link check result, and every tab snapshot from
// src into dst. dst must not already hold any of the workspaces. It returns how many workspaces and entries it copied.
func Migrate(ctx context.Context, src, dst Store) (workspaces, entries int, err error) {
	all, err := src.Workspaces(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, w := range all {
		if _, err := dst.Workspace(ctx, w.ID); err == nil {
			return workspaces, entries, fmt.Errorf("workspace %q already exists in the destination", w.ID)
		}
	}
	for _, w := range all {
		if _, err := dst.CreateWorkspace(ctx, w.ID, w.Name, w.Created); err != nil {
			return workspaces, entries, err
		}
		h, err := src.History(ctx, w.ID)
		if err != nil {
			return workspaces, entries, err
		}
		for _, e := range h {
			tree, _, err := src.State(ctx, w.ID, e.Seq)
			if err != nil {
				return workspaces, entries, err
			}
			if _, err := dst.Commit(ctx, w.ID, e.Seq-1, Change{Op: e.Op, Summary: e.Summary, Tree: tree, Undid: e.Undid, Redone: e.Redone}, e.At); err != nil {
				return workspaces, entries, fmt.Errorf("workspace %s entry %d: %w", w.ID, e.Seq, err)
			}
			entries++
		}
		if err := copySources(ctx, src, dst, w.ID, w.ID); err != nil {
			return workspaces, entries, err
		}
		// Notes, lock and archive go over last: a locked workspace would refuse the entries above.
		if err := dst.UpdateWorkspace(ctx, w); err != nil {
			return workspaces, entries, err
		}
		workspaces++
	}
	// People and their API tokens move too; sessions do not (everyone signs in again after a move).
	users, err := src.Users(ctx)
	if err != nil {
		return workspaces, entries, err
	}
	for _, u := range users {
		if err := dst.CreateUser(ctx, u); err != nil {
			return workspaces, entries, err
		}
	}
	tokens, err := src.Tokens(ctx, "")
	if err != nil {
		return workspaces, entries, err
	}
	for _, t := range tokens {
		if err := dst.CreateToken(ctx, t); err != nil {
			return workspaces, entries, err
		}
	}
	links, err := src.Links(ctx, nil)
	if err != nil {
		return workspaces, entries, err
	}
	results := make([]LinkResult, 0, len(links))
	for _, r := range links {
		results = append(results, r)
	}
	if err := dst.SaveLinks(ctx, results); err != nil {
		return workspaces, entries, err
	}
	if doc, err := src.PrefsDoc(ctx); err != nil {
		return workspaces, entries, err
	} else if doc != nil {
		if err := dst.SavePrefsDoc(ctx, doc); err != nil {
			return workspaces, entries, err
		}
	}
	notes, err := src.TabNotes(ctx)
	if err != nil {
		return workspaces, entries, err
	}
	for _, n := range notes {
		if err := dst.SetTabNote(ctx, n); err != nil {
			return workspaces, entries, err
		}
	}
	tabs, err := src.TabSnapshots(ctx, 0)
	if err != nil {
		return workspaces, entries, err
	}
	for _, t := range tabs {
		full, err := src.TabSnapshot(ctx, t.ID)
		if err != nil {
			return workspaces, entries, err
		}
		if err := dst.SaveTabs(ctx, full); err != nil {
			return workspaces, entries, err
		}
	}
	return workspaces, entries, nil
}

// copySources copies a workspace's sources, with the kept files, from one store to another.
func copySources(ctx context.Context, src, dst Store, fromID, toID string) error {
	list, err := src.SourceList(ctx, fromID)
	if err != nil {
		return err
	}
	for _, x := range list {
		body, err := src.SourceBody(ctx, fromID, x.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := dst.AddSource(ctx, toID, x, body); err != nil {
			return fmt.Errorf("workspace %s source %s: %w", toID, x.ID, err)
		}
	}
	return nil
}

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *JSONStore) notesFile() string { return filepath.Join(s.root, "tab_notes.json") }

func (s *JSONStore) SetTabNote(_ context.Context, n TabNote) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[TabNote](s.notesFile())
	if err != nil {
		return err
	}
	k := n.Kind + "\x00" + n.Key
	if strings.TrimSpace(n.Note) == "" && len(n.Tags) == 0 {
		delete(m, k)
	} else {
		m[k] = n
	}
	return writeMap(s.notesFile(), m)
}

func (s *JSONStore) TabNotes(_ context.Context) ([]TabNote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[TabNote](s.notesFile())
	if err != nil {
		return nil, err
	}
	out := make([]TabNote, 0, len(m))
	for _, n := range m {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Updated.Equal(out[j].Updated) {
			return out[i].Updated.After(out[j].Updated)
		}
		return out[i].Kind+out[i].Key < out[j].Kind+out[j].Key
	})
	return out, nil
}

func (s *JSONStore) DeleteTabs(_ context.Context, ids []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, id := range ids {
		if ValidID(id) != nil {
			continue
		}
		err := os.Remove(filepath.Join(s.root, "tabs", id+".json"))
		if err == nil {
			n++
		} else if !errors.Is(err, os.ErrNotExist) {
			return n, err
		}
	}
	return n, nil
}

// Scrub has nothing to fold back: every snapshot is its own file, replaced or removed whole.
func (s *JSONStore) Scrub(context.Context) error { return nil }

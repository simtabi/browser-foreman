package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
)

func (s *JSONStore) sourcesDir(wsID string) string { return filepath.Join(s.wsDir(wsID), "sources") }

func (s *JSONStore) readSources(wsID string) ([]Source, error) {
	b, err := os.ReadFile(filepath.Join(s.sourcesDir(wsID), "sources.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []Source{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Source
	return out, json.Unmarshal(b, &out)
}

func (s *JSONStore) AddSource(_ context.Context, wsID string, src Source, body []byte) error {
	if err := ValidID(wsID); err != nil {
		return err
	}
	if err := ValidID(src.ID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.wsDir(wsID)); err != nil {
		return ErrNotFound
	}
	list, err := s.readSources(wsID)
	if err != nil {
		return err
	}
	list = slices.DeleteFunc(list, func(x Source) bool { return x.ID == src.ID })
	list = append(list, src)
	if err := os.MkdirAll(s.sourcesDir(wsID), 0o700); err != nil {
		return err
	}
	if body != nil {
		if err := writeAtomic(filepath.Join(s.sourcesDir(wsID), src.ID+".bin"), body); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.sourcesDir(wsID), "sources.json"), b)
}

func (s *JSONStore) SourceList(_ context.Context, wsID string) ([]Source, error) {
	if err := ValidID(wsID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readSources(wsID)
}

func (s *JSONStore) SourceBody(_ context.Context, wsID, id string) ([]byte, error) {
	if ValidID(wsID) != nil || ValidID(id) != nil {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.sourcesDir(wsID), id+".bin"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

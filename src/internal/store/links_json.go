package store

import (
	"context"
	"path/filepath"
)

// The json driver keeps link results in <dir>/json/links.json, keyed by URL.

func (s *JSONStore) linksFile() string { return filepath.Join(s.root, "links.json") }

func (s *JSONStore) SaveLinks(_ context.Context, rs []LinkResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[LinkResult](s.linksFile())
	if err != nil {
		return err
	}
	for _, r := range rs {
		m[r.URL] = r
	}
	return writeMap(s.linksFile(), m)
}

func (s *JSONStore) Links(_ context.Context, urls []string) (map[string]LinkResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[LinkResult](s.linksFile())
	if err != nil || urls == nil {
		return m, err
	}
	out := map[string]LinkResult{}
	for _, u := range urls {
		if r, ok := m[u]; ok {
			out[u] = r
		}
	}
	return out, nil
}

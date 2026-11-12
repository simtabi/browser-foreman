package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
)

const prefsSchema = `
CREATE TABLE IF NOT EXISTS prefs (name TEXT PRIMARY KEY, body BLOB NOT NULL);
`

func (s *SQLiteStore) PrefsDoc(ctx context.Context) ([]byte, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT body FROM prefs WHERE name = 'prefs'`).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

func (s *SQLiteStore) SavePrefsDoc(ctx context.Context, doc []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO prefs (name, body) VALUES ('prefs', ?) ON CONFLICT(name) DO UPDATE SET body = excluded.body`, doc)
	return err
}

func (s *JSONStore) prefsFile() string { return filepath.Join(s.root, "prefs.json") }

func (s *JSONStore) PrefsDoc(context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.prefsFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

func (s *JSONStore) SavePrefsDoc(_ context.Context, doc []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeAtomic(s.prefsFile(), doc)
}

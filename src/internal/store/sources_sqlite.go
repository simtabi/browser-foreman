package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

const sourcesSchema = `
CREATE TABLE IF NOT EXISTS sources (workspace TEXT NOT NULL REFERENCES workspaces(id), id TEXT NOT NULL,
  n INTEGER NOT NULL, doc TEXT NOT NULL, body BLOB, PRIMARY KEY (workspace, id));
`

func (s *SQLiteStore) AddSource(ctx context.Context, wsID string, src Source, body []byte) error {
	if err := ValidID(src.ID); err != nil {
		return err
	}
	if _, err := s.Workspace(ctx, wsID); err != nil {
		return err
	}
	doc, err := json.Marshal(src)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sources (workspace, id, n, doc, body)
		VALUES (?, ?, (SELECT COALESCE(MAX(n), 0) + 1 FROM sources WHERE workspace = ?), ?, ?)
		ON CONFLICT(workspace, id) DO UPDATE SET n = excluded.n, doc = excluded.doc, body = COALESCE(excluded.body, sources.body)`,
		wsID, src.ID, wsID, string(doc), body)
	return err
}

func (s *SQLiteStore) SourceList(ctx context.Context, wsID string) ([]Source, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc FROM sources WHERE workspace = ? ORDER BY n`, wsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var src Source
		if err := json.Unmarshal([]byte(doc), &src); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SourceBody(ctx context.Context, wsID, id string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT body FROM sources WHERE workspace = ? AND id = ?`, wsID, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && b == nil) {
		return nil, ErrNotFound
	}
	return b, err
}

package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const tabNotesSchema = `
CREATE TABLE IF NOT EXISTS tab_notes (kind TEXT NOT NULL, key TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
  tags TEXT NOT NULL DEFAULT '[]', updated TEXT NOT NULL, PRIMARY KEY (kind, key));
`

func (s *SQLiteStore) SetTabNote(ctx context.Context, n TabNote) error {
	if strings.TrimSpace(n.Note) == "" && len(n.Tags) == 0 {
		_, err := s.db.ExecContext(ctx, `DELETE FROM tab_notes WHERE kind = ? AND key = ?`, n.Kind, n.Key)
		return err
	}
	tags, _ := json.Marshal(n.Tags)
	_, err := s.db.ExecContext(ctx, `INSERT INTO tab_notes (kind, key, note, tags, updated) VALUES (?, ?, ?, ?, ?)
		  ON CONFLICT (kind, key) DO UPDATE SET note = excluded.note, tags = excluded.tags, updated = excluded.updated`,
		n.Kind, n.Key, n.Note, string(tags), n.Updated.UTC().Format(ts))
	return err
}

func (s *SQLiteStore) TabNotes(ctx context.Context) ([]TabNote, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, key, note, tags, updated FROM tab_notes ORDER BY updated DESC, kind, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TabNote{}
	for rows.Next() {
		var n TabNote
		var tags, at string
		if err := rows.Scan(&n.Kind, &n.Key, &n.Note, &tags, &at); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tags), &n.Tags)
		n.Updated, _ = time.Parse(tsRead, at)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteTabs(ctx context.Context, ids []string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }() // a no-op once committed
	n := 0
	for _, id := range ids {
		r, err := tx.ExecContext(ctx, `DELETE FROM tabs WHERE id = ?`, id)
		if err != nil {
			return 0, err
		}
		k, _ := r.RowsAffected()
		n += int(k)
	}
	return n, tx.Commit()
}

func (s *SQLiteStore) Scrub(ctx context.Context) error {
	return s.db.scrub(ctx)
}

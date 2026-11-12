package store

import (
	"context"
	"strings"
	"time"
)

const linksSchema = `
CREATE TABLE IF NOT EXISTS links (url TEXT PRIMARY KEY, status TEXT NOT NULL, code INTEGER NOT NULL DEFAULT 0,
  final TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', checked TEXT NOT NULL);
`

func (s *SQLiteStore) SaveLinks(ctx context.Context, rs []LinkResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.PrepareContext(ctx, `INSERT INTO links (url, status, code, final, note, kind, checked) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET status = excluded.status, code = excluded.code, final = excluded.final,
		note = excluded.note, kind = excluded.kind, checked = excluded.checked`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rs {
		if _, err := st.ExecContext(ctx, r.URL, r.Status, r.Code, r.Final, r.Note, r.Kind, r.Checked.UTC().Format(ts)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) Links(ctx context.Context, urls []string) (map[string]LinkResult, error) {
	out := map[string]LinkResult{}
	q, args := `SELECT url, status, code, final, note, kind, checked FROM links`, []any{}
	if urls != nil {
		if len(urls) == 0 {
			return out, nil
		}
		// One query per 500 addresses keeps under SQLite's limit on bound parameters.
		for i := 0; i < len(urls); i += 500 {
			part := urls[i:min(i+500, len(urls))]
			args = args[:0]
			for _, u := range part {
				args = append(args, u)
			}
			if err := s.scanLinks(ctx, out, q+` WHERE url IN (?`+strings.Repeat(",?", len(part)-1)+`)`, args); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return out, s.scanLinks(ctx, out, q, args)
}

func (s *SQLiteStore) scanLinks(ctx context.Context, out map[string]LinkResult, q string, args []any) error {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var r LinkResult
		var checked string
		if err := rows.Scan(&r.URL, &r.Status, &r.Code, &r.Final, &r.Note, &r.Kind, &checked); err != nil {
			return err
		}
		r.Checked, _ = time.Parse(ts, checked)
		out[r.URL] = r
	}
	return rows.Err()
}

package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

const linksSchema = `
CREATE TABLE IF NOT EXISTS links (url TEXT PRIMARY KEY, status TEXT NOT NULL, code INTEGER NOT NULL DEFAULT 0,
  final TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', checked TEXT NOT NULL);
`

// linkColumns are the links table's columns added after it first shipped (see OpenSQLite).
var linkColumns = []string{
	"dead_since TEXT NOT NULL DEFAULT ''",
	"confirmed INTEGER NOT NULL DEFAULT 0",
	"frame TEXT NOT NULL DEFAULT ''",
	"meta TEXT NOT NULL DEFAULT ''",
	"archive TEXT NOT NULL DEFAULT ''",
}

func encodeMeta(m *PageMeta) string {
	if m == nil || *m == (PageMeta{}) {
		return ""
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func decodeMeta(s string) *PageMeta {
	if s == "" {
		return nil
	}
	var m PageMeta
	if json.Unmarshal([]byte(s), &m) != nil {
		return nil
	}
	return &m
}

func since(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(ts)
}

func (s *SQLiteStore) SaveLinks(ctx context.Context, rs []LinkResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // a no-op once committed
	st, err := tx.PrepareContext(ctx, `INSERT INTO links (url, status, code, final, note, kind, checked, dead_since, confirmed, frame, meta, archive)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(url) DO UPDATE SET status = excluded.status, code = excluded.code, final = excluded.final,
		note = excluded.note, kind = excluded.kind, checked = excluded.checked, dead_since = excluded.dead_since,
		confirmed = excluded.confirmed, frame = excluded.frame, meta = excluded.meta, archive = excluded.archive`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rs {
		if _, err := st.ExecContext(ctx, r.URL, r.Status, r.Code, r.Final, r.Note, r.Kind, r.Checked.UTC().Format(ts),
			since(r.DeadSince), r.Confirmed, r.Frame, encodeMeta(r.Meta), r.Archive); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) Links(ctx context.Context, urls []string) (map[string]LinkResult, error) {
	out := map[string]LinkResult{}
	q, args := `SELECT url, status, code, final, note, kind, checked, dead_since, confirmed, frame, meta, archive FROM links`, []any{}
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
		var checked, dead, meta string
		if err := rows.Scan(&r.URL, &r.Status, &r.Code, &r.Final, &r.Note, &r.Kind, &checked, &dead, &r.Confirmed, &r.Frame, &meta, &r.Archive); err != nil {
			return err
		}
		r.Checked, _ = time.Parse(ts, checked)
		if dead != "" {
			r.DeadSince, _ = time.Parse(ts, dead)
		}
		r.Meta = decodeMeta(meta)
		out[r.URL] = r
	}
	return rows.Err()
}

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure Go: no C toolchain, so every target cross-compiles and the universal build works
)

// SQLiteStore keeps everything in <dir>/browser-foreman.sqlite3 (0600), in WAL mode. Each Commit is one transaction.
type SQLiteStore struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS workspaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS workspace_meta (id TEXT PRIMARY KEY REFERENCES workspaces(id), notes TEXT NOT NULL DEFAULT '',
  locked INTEGER NOT NULL DEFAULT 0, archived INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS snapshots (hash TEXT PRIMARY KEY, body BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS entries (
  ws TEXT NOT NULL REFERENCES workspaces(id), seq INTEGER NOT NULL, at TEXT NOT NULL, op TEXT NOT NULL,
  summary TEXT NOT NULL, base INTEGER NOT NULL, snap TEXT NOT NULL REFERENCES snapshots(hash),
  undid INTEGER NOT NULL DEFAULT 0, redone INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (ws, seq));
CREATE TABLE IF NOT EXISTS tabs (id TEXT PRIMARY KEY, at TEXT NOT NULL, reason TEXT NOT NULL, windows BLOB NOT NULL);
CREATE INDEX IF NOT EXISTS tabs_at ON tabs(at);
`

func OpenSQLite(dir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "browser-foreman.sqlite3")
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil { // created private before SQLite opens it
		f.Close()
	} else {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; keeps every Commit's read-check-write in one connection
	if _, err := db.Exec(schema + accountsSchema + linksSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite schema: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

const ts = time.RFC3339Nano

func (s *SQLiteStore) CreateWorkspace(ctx context.Context, id, name string, at time.Time) (Workspace, error) {
	if err := ValidID(id); err != nil {
		return Workspace{}, err
	}
	at = at.UTC()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO workspaces (id, name, created) VALUES (?, ?, ?)`, id, name, at.Format(ts)); err != nil {
		var exists int
		if s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE id = ?`, id).Scan(&exists) == nil && exists > 0 {
			return Workspace{}, fmt.Errorf("workspace %q already exists", id)
		}
		return Workspace{}, err
	}
	return Workspace{ID: id, Name: name, Created: at}, nil
}

func (s *SQLiteStore) Workspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.id, w.name, w.created, COALESCE(MAX(e.seq), 0),
	  COALESCE(m.notes, ''), COALESCE(m.locked, 0), COALESCE(m.archived, 0) FROM workspaces w
	  LEFT JOIN entries e ON e.ws = w.id LEFT JOIN workspace_meta m ON m.id = w.id GROUP BY w.id ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		var created string
		if err := rows.Scan(&w.ID, &w.Name, &created, &w.Head, &w.Notes, &w.Locked, &w.Archived); err != nil {
			return nil, err
		}
		w.Created, _ = time.Parse(ts, created)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Workspace(ctx context.Context, id string) (Workspace, error) {
	var w Workspace
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT w.id, w.name, w.created, COALESCE((SELECT MAX(seq) FROM entries WHERE ws = w.id), 0),
	  COALESCE(m.notes, ''), COALESCE(m.locked, 0), COALESCE(m.archived, 0)
	  FROM workspaces w LEFT JOIN workspace_meta m ON m.id = w.id WHERE w.id = ?`, id).Scan(&w.ID, &w.Name, &created, &w.Head, &w.Notes, &w.Locked, &w.Archived)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	w.Created, _ = time.Parse(ts, created)
	return w, err
}

func (s *SQLiteStore) UpdateWorkspace(ctx context.Context, w Workspace) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET name = ? WHERE id = ?`, w.Name, w.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_meta (id, notes, locked, archived) VALUES (?, ?, ?, ?)
	  ON CONFLICT(id) DO UPDATE SET notes = excluded.notes, locked = excluded.locked, archived = excluded.archived`,
		w.ID, w.Notes, w.Locked, w.Archived); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) Commit(ctx context.Context, id string, base int, c Change, at time.Time) (Entry, error) {
	if err := c.Tree.Validate(); err != nil {
		return Entry{}, err
	}
	hash, body, err := Hash(c.Tree)
	if err != nil {
		return Entry{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	var head, exists, locked int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE((SELECT MAX(seq) FROM entries WHERE ws = ?), 0),
	  COALESCE((SELECT locked FROM workspace_meta WHERE id = ?), 0) FROM workspaces WHERE id = ?`, id, id, id).Scan(&exists, &head, &locked); err != nil {
		return Entry{}, err
	}
	if exists == 0 {
		return Entry{}, ErrNotFound
	}
	if locked != 0 {
		return Entry{}, ErrLocked
	}
	if base != head {
		return Entry{}, fmt.Errorf("%w (made on %d, now %d)", ErrConflict, base, head)
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO snapshots (hash, body) VALUES (?, ?)`, hash, body); err != nil {
		return Entry{}, err
	}
	e := Entry{Seq: head + 1, At: at.UTC(), Op: c.Op, Summary: c.Summary, Base: base, Snap: hash, Undid: c.Undid, Redone: c.Redone}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entries (ws, seq, at, op, summary, base, snap, undid, redone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, e.Seq, e.At.Format(ts), e.Op, e.Summary, e.Base, e.Snap, e.Undid, e.Redone); err != nil {
		return Entry{}, err
	}
	return e, tx.Commit()
}

func (s *SQLiteStore) History(ctx context.Context, id string) ([]Entry, error) {
	if _, err := s.Workspace(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT seq, at, op, summary, base, snap, undid, redone FROM entries WHERE ws = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var at string
		if err := rows.Scan(&e.Seq, &at, &e.Op, &e.Summary, &e.Base, &e.Snap, &e.Undid, &e.Redone); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(ts, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) State(ctx context.Context, id string, seq int) (Tree, Entry, error) {
	q := `SELECT e.seq, e.at, e.op, e.summary, e.base, e.snap, e.undid, e.redone, s.body FROM entries e JOIN snapshots s ON s.hash = e.snap
	  WHERE e.ws = ? AND e.seq = ?`
	if seq == 0 {
		q = `SELECT e.seq, e.at, e.op, e.summary, e.base, e.snap, e.undid, e.redone, s.body FROM entries e JOIN snapshots s ON s.hash = e.snap
	  WHERE e.ws = ? AND e.seq = (SELECT MAX(seq) FROM entries WHERE ws = ?)`
	}
	args := []any{id, seq}
	if seq == 0 {
		args = []any{id, id}
	}
	var e Entry
	var at string
	var body []byte
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&e.Seq, &at, &e.Op, &e.Summary, &e.Base, &e.Snap, &e.Undid, &e.Redone, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return Tree{}, Entry{}, ErrNotFound
	}
	if err != nil {
		return Tree{}, Entry{}, err
	}
	e.At, _ = time.Parse(ts, at)
	var t Tree
	return t, e, json.Unmarshal(body, &t)
}

func (s *SQLiteStore) SaveTabs(ctx context.Context, t TabSnapshot) error {
	if err := ValidID(t.ID); err != nil {
		return err
	}
	w := t.Windows
	if w == nil {
		w = json.RawMessage("null")
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO tabs (id, at, reason, windows) VALUES (?, ?, ?, ?)`, t.ID, t.At.UTC().Format(ts), t.Reason, []byte(w))
	return err
}

func (s *SQLiteStore) TabSnapshots(ctx context.Context, limit int) ([]TabSnapshot, error) {
	q := `SELECT id, at, reason FROM tabs ORDER BY at DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TabSnapshot{}
	for rows.Next() {
		var t TabSnapshot
		var at string
		if err := rows.Scan(&t.ID, &at, &t.Reason); err != nil {
			return nil, err
		}
		t.At, _ = time.Parse(ts, at)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) TabSnapshot(ctx context.Context, id string) (TabSnapshot, error) {
	var t TabSnapshot
	var at string
	var w []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, at, reason, windows FROM tabs WHERE id = ?`, id).Scan(&t.ID, &at, &t.Reason, &w)
	if errors.Is(err, sql.ErrNoRows) {
		return TabSnapshot{}, ErrNotFound
	}
	t.At, _ = time.Parse(ts, at)
	t.Windows = w
	return t, err
}

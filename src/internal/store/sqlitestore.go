package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // the postgres driver
	_ "modernc.org/sqlite"             // pure Go: no C toolchain, so every target cross-compiles and the universal build works
)

// SQLiteStore keeps everything in <dir>/browser-foreman.sqlite3 (0600), in WAL mode. Each Commit is one transaction.
type SQLiteStore struct{ db sqlDB }

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
	conn, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=secure_delete(1)")
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1) // one writer; keeps every Commit's read-check-write in one connection
	return openSQL(sqlDB{DB: conn})
}

// OpenPostgres keeps everything in the Postgres database dsn names, with the same tables as SQLite (BYTEA for BLOB).
// Commits lock their workspace's row, so two servers or processes sharing the database cannot both write one head.
func OpenPostgres(dsn string) (*SQLiteStore, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return openSQL(sqlDB{DB: conn, pg: true})
}

// openSQL makes the tables (and the columns added since the first release) and returns the store.
func openSQL(db sqlDB) (*SQLiteStore, error) {
	all := schema + accountsSchema + linksSchema + prefsSchema + sourcesSchema + tabNotesSchema
	if db.pg {
		all = strings.ReplaceAll(all, " BLOB", " BYTEA")
	}
	if _, err := db.Exec(all); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	// Columns added after the first release: an older database gains them here (the error for one already there is
	// the expected answer and ignored).
	for _, c := range linkColumns {
		_, _ = db.Exec(`ALTER TABLE links ADD COLUMN ` + c)
	}
	_, _ = db.Exec(`ALTER TABLE workspace_meta ADD COLUMN deleted TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE workspace_meta ADD COLUMN color TEXT NOT NULL DEFAULT ''`)
	_, _ = db.Exec(`ALTER TABLE workspace_meta ADD COLUMN icon TEXT NOT NULL DEFAULT ''`)
	// Each snapshot's tab pool, listed with the row; rows saved before it existed take it from their document once.
	if _, err := db.Exec(`ALTER TABLE tabs ADD COLUMN pool TEXT NOT NULL DEFAULT ''`); err == nil && !db.pg {
		_, _ = db.Exec(`UPDATE tabs SET pool = COALESCE(json_extract(windows, '$.workspace'), '') WHERE json_valid(windows)`)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// ts is how times are written: fixed width, so text order is time order ("…:05Z" sorted after "…:05.001Z" with
// RFC3339Nano, which drops trailing zeros). tsRead reads both, so rows written before the change still parse.
const (
	ts     = "2006-01-02T15:04:05.000000000Z07:00"
	tsRead = time.RFC3339Nano
)

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
	  COALESCE(m.notes, ''), COALESCE(m.locked, 0), COALESCE(m.archived, 0), COALESCE(m.deleted, ''),
	  COALESCE(m.color, ''), COALESCE(m.icon, '') FROM workspaces w
	  LEFT JOIN entries e ON e.ws = w.id LEFT JOIN workspace_meta m ON m.id = w.id GROUP BY w.id ORDER BY w.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		var w Workspace
		var created, deleted string
		if err := rows.Scan(&w.ID, &w.Name, &created, &w.Head, &w.Notes, &w.Locked, &w.Archived, &deleted, &w.Color, &w.Icon); err != nil {
			return nil, err
		}
		w.Created, _ = time.Parse(tsRead, created)
		w.Deleted, _ = time.Parse(tsRead, deleted)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) Workspace(ctx context.Context, id string) (Workspace, error) {
	var w Workspace
	var created, deleted string
	err := s.db.QueryRowContext(ctx, `SELECT w.id, w.name, w.created, COALESCE((SELECT MAX(seq) FROM entries WHERE ws = w.id), 0),
	  COALESCE(m.notes, ''), COALESCE(m.locked, 0), COALESCE(m.archived, 0), COALESCE(m.deleted, ''),
	  COALESCE(m.color, ''), COALESCE(m.icon, '')
	  FROM workspaces w LEFT JOIN workspace_meta m ON m.id = w.id WHERE w.id = ?`, id).Scan(&w.ID, &w.Name, &created, &w.Head, &w.Notes, &w.Locked, &w.Archived, &deleted, &w.Color, &w.Icon)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	w.Created, _ = time.Parse(tsRead, created)
	w.Deleted, _ = time.Parse(tsRead, deleted)
	return w, err
}

func (s *SQLiteStore) UpdateWorkspace(ctx context.Context, w Workspace) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // a no-op once committed
	res, err := tx.ExecContext(ctx, `UPDATE workspaces SET name = ? WHERE id = ?`, w.Name, w.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	deleted := ""
	if !w.Deleted.IsZero() {
		deleted = w.Deleted.UTC().Format(ts)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_meta (id, notes, locked, archived, deleted, color, icon) VALUES (?, ?, ?, ?, ?, ?, ?)
	  ON CONFLICT(id) DO UPDATE SET notes = excluded.notes, locked = excluded.locked, archived = excluded.archived, deleted = excluded.deleted,
	  color = excluded.color, icon = excluded.icon`,
		w.ID, w.Notes, w.Locked, w.Archived, deleted, w.Color, w.Icon); err != nil {
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
	defer func() { _ = tx.Rollback() }() // a no-op once committed
	if tx.pg {
		// Postgres runs commits side by side: hold the workspace's row, so the head read below is the one this commit
		// builds on. A second commit on the same workspace waits here, then finds the new head and is refused.
		if _, err := tx.ExecContext(ctx, `SELECT id FROM workspaces WHERE id = ? FOR UPDATE`, id); err != nil {
			return Entry{}, err
		}
	}
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
	packed, err := packSnapshot(body)
	if err != nil {
		return Entry{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO snapshots (hash, body) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING`, hash, packed); err != nil {
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
		e.At, _ = time.Parse(tsRead, at)
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
	e.At, _ = time.Parse(tsRead, at)
	if body, err = unpackSnapshot(body); err != nil {
		return Tree{}, e, err
	}
	var t Tree
	return t, e, json.Unmarshal(body, &t)
}

// packSnapshot gzips a tree's JSON for the snapshots table: a 5,400-bookmark tree is 1.5 MB as JSON and about a tenth
// of that compressed, and every history entry keeps one. The hash stays the SHA-256 of the JSON, not of these bytes.
func packSnapshot(body []byte) ([]byte, error) {
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	if _, err := z.Write(body); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// unpackSnapshot reads a body written by packSnapshot, or a plain JSON one written before snapshots were compressed.
func unpackSnapshot(body []byte) ([]byte, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body, nil
	}
	z, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return io.ReadAll(z)
}

func (s *SQLiteStore) SaveTabs(ctx context.Context, t TabSnapshot) error {
	if err := ValidID(t.ID); err != nil {
		return err
	}
	w := t.Windows
	t.Pool = PoolOf(w) // the document names its pool; the column only lists it
	if w == nil {
		w = json.RawMessage("null")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tabs (id, at, reason, pool, windows) VALUES (?, ?, ?, ?, ?)
	  ON CONFLICT (id) DO UPDATE SET at = excluded.at, reason = excluded.reason, pool = excluded.pool, windows = excluded.windows`, t.ID, t.At.UTC().Format(ts), t.Reason, t.Pool, []byte(w))
	return err
}

func (s *SQLiteStore) TabSnapshots(ctx context.Context, limit int) ([]TabSnapshot, error) {
	q := `SELECT id, at, reason, pool FROM tabs ORDER BY at DESC`
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
		if err := rows.Scan(&t.ID, &at, &t.Reason, &t.Pool); err != nil {
			return nil, err
		}
		t.At, _ = time.Parse(tsRead, at)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) TabSnapshot(ctx context.Context, id string) (TabSnapshot, error) {
	var t TabSnapshot
	var at string
	var w []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, at, reason, pool, windows FROM tabs WHERE id = ?`, id).Scan(&t.ID, &at, &t.Reason, &t.Pool, &w)
	if errors.Is(err, sql.ErrNoRows) {
		return TabSnapshot{}, ErrNotFound
	}
	t.At, _ = time.Parse(tsRead, at)
	t.Windows = w
	return t, err
}

func (s *SQLiteStore) PurgeWorkspace(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // a no-op once committed
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	for _, q := range []string{`DELETE FROM entries WHERE ws = ?`, `DELETE FROM sources WHERE workspace = ?`,
		`DELETE FROM workspace_meta WHERE id = ?`, `DELETE FROM workspaces WHERE id = ?`} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM snapshots WHERE hash NOT IN (SELECT snap FROM entries)`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.db.scrub(ctx)
}

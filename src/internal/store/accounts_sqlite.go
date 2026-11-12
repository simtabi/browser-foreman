package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const accountsSchema = `
CREATE TABLE IF NOT EXISTS users (name TEXT PRIMARY KEY, role TEXT NOT NULL, password_hash TEXT NOT NULL,
  disabled INTEGER NOT NULL DEFAULT 0, created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tokens (id TEXT PRIMARY KEY, user TEXT NOT NULL REFERENCES users(name) ON DELETE CASCADE,
  name TEXT NOT NULL, hash TEXT NOT NULL UNIQUE, created TEXT NOT NULL, last_used TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS sessions (hash TEXT PRIMARY KEY, user TEXT NOT NULL REFERENCES users(name) ON DELETE CASCADE,
  csrf TEXT NOT NULL, created TEXT NOT NULL, expires TEXT NOT NULL);
`

func (s *SQLiteStore) CreateUser(ctx context.Context, u User) error {
	if err := ValidID(u.Name); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (name, role, password_hash, disabled, created) VALUES (?, ?, ?, ?, ?)`,
		u.Name, u.Role, u.PasswordHash, u.Disabled, u.Created.UTC().Format(ts))
	if err != nil {
		if _, e := s.User(ctx, u.Name); e == nil {
			return fmt.Errorf("user %q already exists", u.Name)
		}
	}
	return err
}

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created string
	if err := row.Scan(&u.Name, &u.Role, &u.PasswordHash, &u.Disabled, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	u.Created, _ = time.Parse(ts, created)
	return u, nil
}

func (s *SQLiteStore) User(ctx context.Context, name string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT name, role, password_hash, disabled, created FROM users WHERE name = ?`, name))
}

func (s *SQLiteStore) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, role, password_hash, disabled, created FROM users ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateUser(ctx context.Context, u User) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET role = ?, password_hash = ?, disabled = ? WHERE name = ?`, u.Role, u.PasswordHash, u.Disabled, u.Name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteUser(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE name = ?`, name) // tokens and sessions cascade
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanToken(row interface{ Scan(...any) error }) (Token, error) {
	var t Token
	var created, used string
	if err := row.Scan(&t.ID, &t.User, &t.Name, &t.Hash, &created, &used); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Token{}, ErrNotFound
		}
		return Token{}, err
	}
	t.Created, _ = time.Parse(ts, created)
	t.LastUsed, _ = time.Parse(ts, used)
	return t, nil
}

func (s *SQLiteStore) CreateToken(ctx context.Context, t Token) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tokens (id, user, name, hash, created) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.User, t.Name, t.Hash, t.Created.UTC().Format(ts))
	return err
}

func (s *SQLiteStore) TokenByHash(ctx context.Context, hash string) (Token, error) {
	return scanToken(s.db.QueryRowContext(ctx, `SELECT id, user, name, hash, created, last_used FROM tokens WHERE hash = ?`, hash))
}

func (s *SQLiteStore) Tokens(ctx context.Context, user string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user, name, hash, created, last_used FROM tokens WHERE ? = '' OR user = ? ORDER BY created`, user, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) TouchToken(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tokens SET last_used = ? WHERE id = ?`, at.UTC().Format(ts), id)
	return err
}

func (s *SQLiteStore) RevokeToken(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tokens WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) CreateSession(ctx context.Context, x Session) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires < ?`, time.Now().UTC().Format(ts)); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (hash, user, csrf, created, expires) VALUES (?, ?, ?, ?, ?)`,
		x.Hash, x.User, x.CSRF, x.Created.UTC().Format(ts), x.Expires.UTC().Format(ts))
	return err
}

func (s *SQLiteStore) SessionByHash(ctx context.Context, hash string, now time.Time) (Session, error) {
	var x Session
	var created, expires string
	err := s.db.QueryRowContext(ctx, `SELECT hash, user, csrf, created, expires FROM sessions WHERE hash = ?`, hash).Scan(&x.Hash, &x.User, &x.CSRF, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	x.Created, _ = time.Parse(ts, created)
	x.Expires, _ = time.Parse(ts, expires)
	if !x.Expires.After(now) {
		return Session{}, ErrNotFound
	}
	return x, nil
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE hash = ?`, hash)
	return err
}

func (s *SQLiteStore) DeleteSessions(ctx context.Context, user, keep string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user = ? AND hash != ?`, user, keep)
	return err
}

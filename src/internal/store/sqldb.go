package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// sqlDB is the database the SQL driver talks to, in SQLite's or Postgres's dialect. The statements are written once,
// with ? placeholders; for Postgres they are numbered ($1, $2, …) on the way out.
type sqlDB struct {
	*sql.DB
	pg bool
}

func (d sqlDB) Exec(q string, a ...any) (sql.Result, error) {
	return d.DB.Exec(d.bind(q), args(a, d.pg)...)
}
func (d sqlDB) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, d.bind(q), args(a, d.pg)...)
}
func (d sqlDB) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, d.bind(q), args(a, d.pg)...)
}
func (d sqlDB) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, d.bind(q), args(a, d.pg)...)
}
func (d sqlDB) BeginTx(ctx context.Context, o *sql.TxOptions) (sqlTx, error) {
	tx, err := d.DB.BeginTx(ctx, o)
	return sqlTx{tx, d.pg}, err
}
func (d sqlDB) bind(q string) string { return bind(q, d.pg) }

// sqlTx is a transaction on a sqlDB, numbering placeholders the same way.
type sqlTx struct {
	*sql.Tx
	pg bool
}

func (t sqlTx) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, bind(q, t.pg), args(a, t.pg)...)
}
func (t sqlTx) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, bind(q, t.pg), args(a, t.pg)...)
}
func (t sqlTx) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, bind(q, t.pg), args(a, t.pg)...)
}
func (t sqlTx) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return t.Tx.PrepareContext(ctx, bind(q, t.pg))
}

// args sends Go booleans to Postgres as 0 and 1: the columns are INTEGER, as SQLite keeps them, and Postgres does not
// turn a boolean into an integer by itself. Reading them back into a bool works for both.
func args(a []any, pg bool) []any {
	if !pg {
		return a
	}
	out := make([]any, len(a))
	for i, v := range a {
		if b, ok := v.(bool); ok {
			v = 0
			if b {
				v = 1
			}
		}
		out[i] = v
	}
	return out
}

// bind numbers the ? placeholders of q for Postgres ($1, $2, …), leaving any ? inside a quoted string alone.
func bind(q string, pg bool) string {
	if !pg || !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	n, quoted := 0, false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'':
			quoted = !quoted
		case c == '?' && !quoted:
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// scrub makes deleted rows unreadable from the database's files: SQLite folds its write-ahead log back in (with
// secure_delete on, freed pages are zeroed); Postgres vacuums, freeing the dead rows' space for reuse.
func (d sqlDB) scrub(ctx context.Context) error {
	if d.pg {
		_, err := d.DB.ExecContext(ctx, `VACUUM`)
		return err
	}
	_, err := d.DB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

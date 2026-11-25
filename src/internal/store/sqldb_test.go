package store

import "testing"

func TestBindNumbersPlaceholdersForPostgresOnly(t *testing.T) {
	q := `SELECT a FROM t WHERE x = ? AND y = '?' AND z IN (?, ?)`
	if got := bind(q, false); got != q {
		t.Fatalf("sqlite changed: %s", got)
	}
	if got, want := bind(q, true), `SELECT a FROM t WHERE x = $1 AND y = '?' AND z IN ($2, $3)`; got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

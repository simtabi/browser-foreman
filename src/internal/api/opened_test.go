package api

import (
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/history"
	"github.com/simtabi/browser-foreman/src/internal/merge"
)

func TestEveryBookmarkSharingAPageIsCredited(t *testing.T) {
	k := merge.Key("https://go.dev/")
	byKey := map[string][]string{k: {"http://go.dev", "https://go.dev/"}}
	early, late := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	out := map[string]*seen{}
	creditVisits(out, byKey, []history.Visit{{URL: "https://go.dev", Last: early, Visits: 2}, {URL: "http://go.dev/", Last: late, Visits: 3}})
	for _, u := range byKey[k] {
		if o := out[u]; o == nil || o.Visits != 5 || !o.Last.Equal(late) {
			t.Fatalf("%s: %+v, want 5 visits, last %v", u, o, late)
		}
	}
	if len(out) != 2 {
		t.Fatalf("credited %d addresses, want 2", len(out))
	}
}

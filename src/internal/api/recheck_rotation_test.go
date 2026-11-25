package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// A scheduled re-check that cannot reach a site keeps what was known about each link, but still counts as an attempt:
// otherwise those links stay due for ever, every round picks the same workspace again, and the workspaces after it
// are never re-checked.
func TestAnUnreachableRecheckStillMovesOn(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests) // "slow down": the check cannot tell anything
	}))
	defer site.Close()
	s := newServer(t, apiTok)
	ctx := context.Background()
	linkWorkspace(t, s, site.URL)
	old := time.Now().Add(-40 * 24 * time.Hour)
	urls := []string{site.URL + "/a", site.URL + "/b"}
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{
		{URL: urls[0], Status: "ok", Checked: old},
		{URL: urls[1], Status: "ok", Checked: old},
	})
	cutoff := time.Now().Add(-time.Hour)
	s.recheckStale(ctx)
	var got map[string]store.LinkResult
	for range 200 {
		got, _ = s.Data.Links(ctx, urls)
		if got[urls[0]].Checked.After(cutoff) && got[urls[1]].Checked.After(cutoff) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, u := range urls {
		l := got[u]
		if !l.Checked.After(cutoff) {
			t.Fatalf("%s was not marked as tried, so it stays due: %+v", u, l)
		}
		if l.Status != "ok" {
			t.Fatalf("%s: an unreachable check replaced what was known: %+v", u, l)
		}
	}
}

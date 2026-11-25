package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	notifykit "github.com/simtabi/go-notifykit/src"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func recorded(t *testing.T) (*notifykit.Notifier, *notifykit.Recorder) {
	t.Helper()
	rec := &notifykit.Recorder{}
	n, err := notifykit.New(notifykit.Config{AppName: "browser-foreman", AppID: "com.simtabi.browser-foreman", Backend: rec})
	if err != nil {
		t.Fatal(err)
	}
	return n, rec
}

// waitSent waits for the sweep's goroutine to hand the recorder what it sends.
func waitSent(rec *notifykit.Recorder, want int) []notifykit.Notification {
	for range 200 {
		if got := rec.Sent(); len(got) >= want {
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
	return rec.Sent()
}

func TestAScheduledRecheckNotifiesOnceAboutConfirmedDeadLinks(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer site.Close()
	s := newServer(t, apiTok)
	var rec *notifykit.Recorder
	s.Notifier, rec = recorded(t)
	ctx := context.Background()
	linkWorkspace(t, s, site.URL)
	old := time.Now().Add(-10 * 24 * time.Hour)
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{
		{URL: site.URL + "/a", Status: "dead", Checked: old, DeadSince: old}, // suspected dead ten days ago: confirmed now
		{URL: site.URL + "/b", Status: "ok", Checked: old},                   // dead for the first time: only suspected
	})
	s.recheckStale(ctx)
	sent := waitSent(rec, 1)
	if len(sent) != 1 {
		t.Fatalf("%d notifications: %+v", len(sent), sent)
	}
	if n := sent[0]; n.Title != "1 link stopped working" || n.Group != "linkcheck" || n.Level != notifykit.Warn {
		t.Fatalf("%+v", n)
	}
}

func TestACheckStartedFromThePageDoesNotNotify(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer site.Close()
	s := newServer(t, apiTok)
	var rec *notifykit.Recorder
	s.Notifier, rec = recorded(t)
	ctx := context.Background()
	linkWorkspace(t, s, site.URL)
	old := time.Now().Add(-10 * 24 * time.Hour)
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{{URL: site.URL + "/a", Status: "dead", Checked: old, DeadSince: old}})
	job, err := s.StartCheck("links", []string{site.URL + "/a"})
	if err != nil {
		t.Fatal(err)
	}
	job.Wait()
	time.Sleep(200 * time.Millisecond)
	if got := rec.Sent(); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

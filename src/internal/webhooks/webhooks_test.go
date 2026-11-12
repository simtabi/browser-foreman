package webhooks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/events"
)

func TestSignVerifyRoundTripAndTampering(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"id":1}`)
	h := Sign("s3cret-s3cret-s3cret", now, body)
	if err := Verify("s3cret-s3cret-s3cret", h, body, now, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if Verify("s3cret-s3cret-s3cret", h, []byte(`{"id":2}`), now, 5*time.Minute) == nil {
		t.Fatal("changed body accepted")
	}
	if Verify("wrong-wrong-wrong-wrong", h, body, now, 5*time.Minute) == nil {
		t.Fatal("wrong secret accepted")
	}
	if Verify("s3cret-s3cret-s3cret", h, body, now.Add(10*time.Minute), 5*time.Minute) == nil {
		t.Fatal("old signature accepted")
	}
}

func TestCheckURLRefusesDangerousTargets(t *testing.T) {
	for _, u := range []string{"ftp://x/", "http://169.254.169.254/latest", "http://metadata.google.internal/", "http://user:pw@example.com/", "http://[fe80::1]/", "http://0.0.0.0/", "notaurl"} {
		if CheckURL(u) == nil {
			t.Errorf("%s accepted", u)
		}
	}
	for _, u := range []string{"https://hooks.example.com/brofo", "http://127.0.0.1:9000/cb", "http://localhost/cb"} {
		if err := CheckURL(u); err != nil {
			t.Errorf("%s refused: %v", u, err)
		}
	}
}

func TestPatterns(t *testing.T) {
	s := Subscription{Events: []string{"bookmark.*", "tabs.snapshot.saved"}}
	for typ, want := range map[string]bool{"bookmark.created": true, "bookmark.deleted": true, "bookmarks": false, "tabs.snapshot.saved": true, "tabs.closed": false} {
		if s.Matches(typ) != want {
			t.Errorf("%s: want %v", typ, want)
		}
	}
	if !(Subscription{Events: []string{"*"}}).Matches("anything") {
		t.Error("* should match everything")
	}
}

func TestDeliverSignsRetriesAndStopsOnRefusal(t *testing.T) {
	var calls atomic.Int32
	secret := "0123456789abcdef0123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if err := Verify(secret, r.Header.Get("X-Brofo-Signature"), body, time.Now(), time.Minute); err != nil {
			t.Errorf("bad signature: %v", err)
		}
		if r.Header.Get("X-Brofo-Event") != "bookmark.created" {
			t.Errorf("event header %q", r.Header.Get("X-Brofo-Event"))
		}
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // fails twice, then succeeds
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	st := NewStore(t.TempDir())
	if _, err := st.Add(Subscription{ID: "a", URL: srv.URL, Events: []string{"bookmark.*"}, Secret: secret}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Add(Subscription{ID: "b", URL: srv.URL, Events: []string{"tabs.*"}, Secret: secret}); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{Store: st, Client: srv.Client(), Attempts: 5, Backoff: time.Millisecond, Now: time.Now}
	bus := events.New(10, time.Now)
	e, _ := bus.Publish("bookmark.created", map[string]int{"id": 1})
	got := d.Deliver(context.Background(), e)
	if len(got) != 1 || !got[0].OK || got[0].Attempts != 3 || calls.Load() != 3 {
		t.Fatalf("deliveries %+v, calls %d", got, calls.Load())
	}
	refuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusGone) }))
	defer refuse.Close()
	calls.Store(0)
	r := d.SendOne(context.Background(), Subscription{ID: "c", URL: refuse.URL, Secret: secret}, e)
	if r.OK || r.Attempts != 1 || calls.Load() != 1 {
		t.Fatalf("a 410 must not be retried: %+v calls %d", r, calls.Load())
	}
}

func TestStoreValidatesAndRemoves(t *testing.T) {
	st := NewStore(t.TempDir())
	if _, err := st.Add(Subscription{ID: "x", URL: "http://169.254.169.254/", Events: []string{"*"}, Secret: "0123456789abcdef"}); err == nil {
		t.Fatal("metadata address accepted")
	}
	if _, err := st.Add(Subscription{ID: "x", URL: "https://a.example/", Events: []string{"*"}, Secret: "short"}); err == nil {
		t.Fatal("short secret accepted")
	}
	if _, err := st.Add(Subscription{ID: "x", URL: "https://a.example/", Events: []string{"*"}, Secret: "0123456789abcdef"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := st.Remove("x"); !ok {
		t.Fatal("remove failed")
	}
	if ok, _ := st.Remove("x"); ok {
		t.Fatal("removed twice")
	}
}

package webhooks

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/events"
)

// The address check runs when connecting, not only on the address as typed: a name that resolves to the metadata
// service, or any route there, is refused before a byte is sent.
func TestTheClientRefusesMetadataAddressesWhenConnecting(t *testing.T) {
	d := &Dispatcher{Client: NewClient(2 * time.Second), Attempts: 1, Backoff: time.Millisecond, Now: time.Now}
	rec := d.SendOne(context.Background(), Subscription{ID: "x", URL: "http://169.254.169.254/latest/meta-data/", Secret: rand.Text()}, events.Event{ID: 1, Type: "t"})
	if rec.OK || !strings.Contains(rec.Error, "refused") {
		t.Fatalf("%+v", rec)
	}
}

// A redirect is not followed: the receiver's answer is the answer, and a redirect is a refusal, not tried again.
func TestRedirectsAreNotFollowed(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Redirect(w, r, "http://169.254.169.254/", http.StatusFound)
	}))
	defer srv.Close()
	d := &Dispatcher{Client: NewClient(2 * time.Second), Attempts: 3, Backoff: time.Millisecond, Now: time.Now}
	rec := d.SendOne(context.Background(), Subscription{ID: "x", URL: srv.URL, Secret: rand.Text()}, events.Event{ID: 1, Type: "t"})
	if rec.OK || rec.Status != http.StatusFound || rec.Attempts != 1 || hits != 1 {
		t.Fatalf("followed or retried a redirect: %+v hits %d", rec, hits)
	}
}

// Waits double, but never beyond maxWait: twenty attempts used to mean about twelve days.
func TestRetryWaitsAreCapped(t *testing.T) {
	w := time.Second
	for range 30 {
		w = nextWait(w)
	}
	if w != maxWait {
		t.Fatalf("wait grew to %v", w)
	}
	if nextWait(2*time.Second) != 4*time.Second {
		t.Fatal("waits no longer double")
	}
}

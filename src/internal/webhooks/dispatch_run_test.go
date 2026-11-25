package webhooks

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/events"
)

// A receiver that never answers must not hold up the others, and what cannot be queued for it is recorded, not lost
// without a trace.
func TestOneDeadReceiverDoesNotStallTheOthers(t *testing.T) {
	secret := rand.Text() // generated per run, never a fixed value
	stuck := make(chan struct{})
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // holds every request until the test ends
		case <-stuck:
		case <-r.Context().Done():
		}
	}))
	defer dead.Close()
	defer close(stuck)
	var fast atomic.Int32
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fast.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer live.Close()

	st := NewStore(t.TempDir())
	for id, u := range map[string]string{"dead": dead.URL, "live": live.URL} {
		if _, err := st.Add(Subscription{ID: id, URL: u, Events: []string{"*"}, Secret: secret}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var dropped []Delivery
	d := &Dispatcher{Store: st, Client: &http.Client{}, Attempts: 1, Backoff: time.Millisecond, Now: time.Now,
		Log: func(rec Delivery) {
			if strings.HasPrefix(rec.Error, "not sent") {
				mu.Lock()
				dropped = append(dropped, rec)
				mu.Unlock()
			}
		}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan events.Event)
	done := make(chan struct{})
	go func() { d.Run(ctx, ch); close(done) }()

	n := queueSize + 10
	for i := range n {
		ch <- events.Event{ID: uint64(i + 1), Type: "bookmark.created", At: time.Now()}
	}
	deadline := time.Now().Add(5 * time.Second)
	for fast.Load() < int32(n) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := fast.Load(); got != int32(n) {
		t.Fatalf("the live receiver got %d of %d events while another was stuck", got, n)
	}
	mu.Lock()
	lost := len(dropped)
	mu.Unlock()
	// The stuck worker holds one event and its queue holds queueSize more; the rest are recorded as not sent.
	if lost != n-queueSize-1 || dropped[0].Subscription != "dead" {
		t.Fatalf("recorded %d not-sent deliveries, want %d: %+v", lost, n-queueSize-1, dropped)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
}

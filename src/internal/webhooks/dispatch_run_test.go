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

type stuckReceiver struct {
	srv     *httptest.Server
	arrived chan struct{} // one value per request that reached it
	release chan struct{}
}

func newStuck(t *testing.T) *stuckReceiver {
	r := &stuckReceiver{arrived: make(chan struct{}, 4096), release: make(chan struct{})}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.arrived <- struct{}{}
		select { // never answers until the test lets go
		case <-r.release:
		case <-req.Context().Done():
		}
	}))
	t.Cleanup(func() { close(r.release); r.srv.Close() })
	return r
}

func runDispatcher(t *testing.T, st *Store, log func(Delivery)) chan<- events.Event {
	t.Helper()
	d := &Dispatcher{Store: st, Client: &http.Client{}, Attempts: 1, Backoff: time.Millisecond, Now: time.Now, Log: log}
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan events.Event)
	done := make(chan struct{})
	go func() { d.Run(ctx, ch); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Run did not stop when its context ended")
		}
	})
	return ch
}

func event(i int) events.Event {
	return events.Event{ID: uint64(i), Type: "bookmark.created", At: time.Now()}
}

// A receiver that never answers must not hold up the others.
func TestOneDeadReceiverDoesNotStallTheOthers(t *testing.T) {
	secret := rand.Text() // generated per run, never a fixed value
	dead := newStuck(t)
	var fast atomic.Int32
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fast.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer live.Close()
	st := NewStore(t.TempDir())
	for id, u := range map[string]string{"dead": dead.srv.URL, "live": live.URL} {
		if _, err := st.Add(Subscription{ID: id, URL: u, Events: []string{"*"}, Secret: secret}); err != nil {
			t.Fatal(err)
		}
	}
	ch := runDispatcher(t, st, nil)
	const n = 20
	for i := 1; i <= n; i++ {
		ch <- event(i)
	}
	deadline := time.Now().Add(5 * time.Second)
	for fast.Load() < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := fast.Load(); got != n {
		t.Fatalf("the live receiver got %d of %d events while another was stuck", got, n)
	}
}

// What cannot be queued for a webhook is recorded as not sent, never lost without a trace.
func TestAFullQueueIsRecordedNotLost(t *testing.T) {
	dead := newStuck(t)
	st := NewStore(t.TempDir())
	if _, err := st.Add(Subscription{ID: "dead", URL: dead.srv.URL, Events: []string{"*"}, Secret: rand.Text()}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var notSent []Delivery
	ch := runDispatcher(t, st, func(rec Delivery) {
		if strings.HasPrefix(rec.Error, "not sent") && rec.Event != 0 { // 0 is the sync marker below
			mu.Lock()
			notSent = append(notSent, rec)
			mu.Unlock()
		}
	})
	ch <- event(1)
	select { // the worker now holds event 1, so the queue is empty
	case <-dead.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("the first event never reached the receiver")
	}
	for i := 2; i <= queueSize+3; i++ { // queueSize fit; the last two do not
		ch <- event(i)
	}
	ch <- events.Event{ID: 0, Type: "noop.sync"} // Run has handled everything before this once it takes it
	mu.Lock()
	defer mu.Unlock()
	if len(notSent) != 2 || notSent[0].Event != uint64(queueSize+2) || notSent[0].Subscription != "dead" {
		t.Fatalf("recorded %d not-sent deliveries, want 2 (events %d and %d): %+v", len(notSent), queueSize+2, queueSize+3, notSent)
	}
}

// Events queued for a webhook that is then removed are not sent to it.
func TestARemovedWebhookGetsNothingMoreFromItsQueue(t *testing.T) {
	st := NewStore(t.TempDir())
	stuck := newStuck(t)
	if _, err := st.Add(Subscription{ID: "gone", URL: stuck.srv.URL, Events: []string{"*"}, Secret: rand.Text(), Active: true}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var logged []Delivery
	ch := runDispatcher(t, st, func(d Delivery) { mu.Lock(); logged = append(logged, d); mu.Unlock() })
	for i := 1; i <= 3; i++ {
		ch <- event(i)
	}
	<-stuck.arrived // the first is in flight; the other two wait in its queue
	if ok, err := st.Remove("gone"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	stuck.release <- struct{}{} // lets the first one finish
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(logged)
		mu.Unlock()
		if n >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 3 {
		t.Fatalf("%d deliveries logged", len(logged))
	}
	for _, d := range logged[1:] {
		if !strings.Contains(d.Error, "removed or turned off") || d.Attempts != 0 {
			t.Errorf("queued after removal: %+v", d)
		}
	}
	if len(stuck.arrived) != 0 {
		t.Errorf("%d more requests reached the removed webhook", len(stuck.arrived))
	}
}

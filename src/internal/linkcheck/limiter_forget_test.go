package linkcheck

import (
	"strconv"
	"testing"
	"time"
)

// The limiter does not remember every site for the server's life, but never forgets one that asked to slow down.
func TestTheLimiterForgetsOnlyQuietSites(t *testing.T) {
	l := NewLimiter()
	now := time.Now()
	l.Now = func() time.Time { return now }
	paused := l.state("slow.example")
	paused.cool, paused.backoff = now.Add(time.Hour), time.Minute
	for i := range maxSites + 10 {
		l.state("s" + strconv.Itoa(i) + ".example")
	}
	if len(l.sites) > maxSites {
		t.Fatalf("remembers %d sites", len(l.sites))
	}
	if l.sites["slow.example"] != paused {
		t.Fatal("a paused site was forgotten, so its pause would be lost")
	}
}

// A site someone is acquiring is never forgotten: Release then found a fresh state with an empty slot channel and
// waited on it for ever, so the check never ended and no other check could start.
func TestASiteBeingAcquiredIsNotForgotten(t *testing.T) {
	l := NewLimiter()
	l.MaxWait = time.Second
	held := l.hold("busy.example") // between AcquireFor's lookup and its taking the slot
	for i := range maxSites + 10 {
		l.state("s" + strconv.Itoa(i) + ".example")
	}
	if l.sites["busy.example"] != held {
		t.Fatal("a site being acquired was forgotten")
	}
	held.sem <- struct{}{} // AcquireFor takes the slot
	done := make(chan struct{})
	go func() { l.Release("busy.example", OK, 0); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Release waited on another state's slot")
	}
	if held.held != 0 || len(held.sem) != 0 {
		t.Fatalf("held %d, slots taken %d", held.held, len(held.sem))
	}
}

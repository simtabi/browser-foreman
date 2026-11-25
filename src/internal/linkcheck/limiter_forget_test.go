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

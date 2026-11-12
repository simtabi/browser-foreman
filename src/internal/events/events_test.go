package events

import (
	"strings"
	"testing"
	"time"
)

func TestPublishFansOutAndReplaysByID(t *testing.T) {
	b := New(3, func() time.Time { return time.Unix(100, 0) })
	ch, stop := b.Subscribe(10)
	defer stop()
	for i := 0; i < 5; i++ {
		if _, err := b.Publish("bookmark.created", map[string]int{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(ch); got != 5 {
		t.Fatalf("subscriber got %d", got)
	}
	missed, lost := b.Since(3)
	if len(missed) != 2 || missed[0].ID != 4 || lost {
		t.Fatalf("since 3: %v lost=%v", missed, lost)
	}
	if _, lost := b.Since(0); !lost {
		t.Fatal("events 1-2 fell out of a buffer of 3: should report lost")
	}
}

func TestSlowSubscriberNeverBlocksPublish(t *testing.T) {
	b := New(10, time.Now)
	_, stop := b.Subscribe(1)
	defer stop()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			_, _ = b.Publish("x", nil)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a full subscriber")
	}
}

func TestSSEFormat(t *testing.T) {
	b := New(1, func() time.Time { return time.Unix(0, 0) })
	e, _ := b.Publish("tabs.snapshot.saved", map[string]string{"id": "s1"})
	s := string(e.SSE())
	if !strings.HasPrefix(s, "id: 1\nevent: tabs.snapshot.saved\ndata: {") || !strings.HasSuffix(s, "\n\n") {
		t.Fatalf("%q", s)
	}
}

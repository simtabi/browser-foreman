// Package events is Browser Foreman's in-process event bus. Every change is published as an Event; the REST API
// streams them as Server-Sent Events and the webhook dispatcher delivers them to subscribers. A ring buffer keeps the
// most recent events so a client that reconnects with Last-Event-ID misses nothing still in it.
package events

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// Event is one thing that happened. ID increases by one per event for the life of the process.
type Event struct {
	ID   uint64          `json:"id"`
	Type string          `json:"type"` // e.g. bookmark.created, workspace.committed, tabs.snapshot.saved
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Bus fans events out to subscribers. Publishing never blocks: a subscriber that falls behind its buffer drops events
// (and can catch up from the ring buffer by ID).
type Bus struct {
	mu   sync.Mutex
	next uint64
	ring []Event
	size int
	subs map[chan Event]struct{}
	now  func() time.Time
}

// New makes a bus keeping the last `keep` events for replay.
func New(keep int, now func() time.Time) *Bus {
	if keep < 1 {
		keep = 1
	}
	return &Bus{size: keep, subs: map[chan Event]struct{}{}, now: now}
}

// Publish records an event and hands it to every subscriber. data is encoded as JSON.
func (b *Bus) Publish(typ string, data any) (Event, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	e := Event{ID: b.next, Type: typ, At: b.now().UTC(), Data: raw}
	b.ring = append(b.ring, e)
	if len(b.ring) > b.size {
		b.ring = b.ring[len(b.ring)-b.size:]
	}
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // a slow subscriber drops this one; it can replay by ID
		}
	}
	return e, nil
}

// Subscribe returns a channel of new events and a function that ends the subscription.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	ch := make(chan Event, buffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}

// Since returns the kept events after id, oldest first, and whether any were lost (id fell out of the buffer).
func (b *Bus) Since(id uint64) ([]Event, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Event
	for _, e := range b.ring {
		if e.ID > id {
			out = append(out, e)
		}
	}
	lost := len(b.ring) > 0 && id+1 < b.ring[0].ID
	return out, lost
}

// SSE formats an event as one Server-Sent Events message.
func (e Event) SSE() []byte {
	body, _ := json.Marshal(e)
	return []byte("id: " + strconv.FormatUint(e.ID, 10) + "\nevent: " + e.Type + "\ndata: " + string(body) + "\n\n")
}

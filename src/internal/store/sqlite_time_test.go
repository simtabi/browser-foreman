package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Times are stored as text and sorted as text, so they must all be the same width: "…:05Z" sorted after
// "…:05.001Z", putting a snapshot from a millisecond earlier first.
func TestSnapshotsWithinOneSecondKeepTheirOrder(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, id := range []string{"whole", "later"} {
			at := t0.Add(time.Duration(i) * time.Millisecond) // t0 has no fraction; the second has one
			if err := s.SaveTabs(ctx, TabSnapshot{ID: id, At: at, Reason: "auto", Windows: json.RawMessage(`[]`)}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := s.TabSnapshots(ctx, 0)
		if err != nil || len(got) != 2 || got[0].ID != "later" {
			t.Fatalf("%v %+v", err, got)
		}
	})
}

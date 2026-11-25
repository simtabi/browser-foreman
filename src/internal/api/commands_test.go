package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A write-back nobody waits for any more (it timed out, or the page went away) is never handed to the extension
// afterwards: it would be applied with no read-back, after the person was told it failed.
func TestACommandNobodyWaitsForIsNotRunLater(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	chrome := paired(t, s, h, "Google Chrome")
	_, err := serverExtension{s}.Run(context.Background(), "Google Chrome", map[string]any{"kind": "write-bookmarks", "workspace": "main"}, "write", 50*time.Millisecond)
	if err == nil {
		t.Fatal("no answer, yet no error")
	}
	var got struct{ Commands []json.RawMessage }
	_ = json.Unmarshal(fromExtension(h, "GET", "/api/v1/extension/commands?wait=0", "", chrome).Body.Bytes(), &got)
	if len(got.Commands) != 0 {
		t.Fatalf("the extension was still given it: %s", got.Commands)
	}
}


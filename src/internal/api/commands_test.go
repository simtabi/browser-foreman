package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

// A write-back nobody waits for any more (it timed out, or the page went away) is never handed to the extension
// afterwards: it would be applied with no read-back, after the person was told it failed.
func TestACommandNobodyWaitsForIsNotRunLater(t *testing.T) {
	s := newServer(t, apiTok)
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

// brokenWriter loses the answer, as a dropped connection does.
type brokenWriter struct{ httptest.ResponseRecorder }

func (b *brokenWriter) Write([]byte) (int, error) { return 0, errors.New("connection reset") }

// Commands whose answer never reached the extension wait for its next poll instead of being marked sent and lost.
func TestCommandsWhoseAnswerWasLostAreQueuedAgain(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	chrome := paired(t, s, h, "Google Chrome")
	if w := send(h, "POST", "/api/v1/extension/commands", `{"browser":"Google Chrome","kind":"open","urls":["https://go.dev/"]}`); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "/api/v1/extension/commands?wait=0", nil)
	r.Host, r.RemoteAddr = host, "127.0.0.1:50000"
	r.Header.Set("Origin", "chrome-extension://abcdefghijklmnopabcdefghijklmnop")
	r.Header.Set("Authorization", "Bearer "+chrome)
	h.ServeHTTP(&brokenWriter{*httptest.NewRecorder()}, r)
	var got struct{ Commands []struct{ Kind string } }
	_ = json.Unmarshal(fromExtension(h, "GET", "/api/v1/extension/commands?wait=0", "", chrome).Body.Bytes(), &got)
	if len(got.Commands) != 1 || got.Commands[0].Kind != "open" {
		t.Fatalf("after a lost answer: %+v", got)
	}
}

package api

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A webhook's address is often a secret in itself (Slack, Discord): only the owner's GET /webhooks may show it, never
// the event stream every viewer and every "*" webhook receives.
func TestWebhookCreatedDoesNotCarryItsAddress(t *testing.T) {
	s := newServer(t, "tok")
	ch, stop := s.Bus.Subscribe(4)
	defer stop()
	target := "https://hooks.example.com/" + rand.Text()
	body := `{"url":"` + target + `","events":["*"],"secret":"` + rand.Text() + `"}`
	if w := send(s.Handler(), "POST", "/api/v1/webhooks", body); w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	select {
	case e := <-ch:
		b, _ := json.Marshal(e)
		if e.Type != "webhook.created" || strings.Contains(string(b), target) || strings.Contains(string(b), "hooks.example.com") {
			t.Fatalf("the event shows the webhook's address: %s", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no webhook.created event")
	}
}


package api

import (
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
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

// Asking to connect is for an extension on this computer; one elsewhere pairs with a code. The Origin header alone
// proves nothing: any program can send it.
func TestOnlyThisComputerMayAskToConnect(t *testing.T) {
	h := newServer(t, "tok").Handler()
	for addr, refused := range map[string]bool{"192.0.2.7:5000": true, "127.0.0.1:5000": false} {
		r := httptest.NewRequest("POST", "/api/v1/extension/requests", strings.NewReader(`{"browser":"Google Chrome","version":"0.1.8"}`))
		r.Host, r.RemoteAddr = host, addr
		r.Header.Set("Origin", "chrome-extension://abcdefghijklmnopabcdefghijklmnop")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if refused != (w.Code == 403) {
			t.Errorf("from %s: %d %s", addr, w.Code, w.Body)
		}
	}
}

// Every phase of a request has a limit, so slow clients cannot pile up connections.
func TestTheServerTimesOutSlowClients(t *testing.T) {
	srv := newServer(t, "tok").httpServer()
	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("header %v, read %v, idle %v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Fatal("a write timeout would cut the event stream and long polls")
	}
}

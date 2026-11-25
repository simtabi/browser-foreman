package api

import (
	"crypto/rand"
	"encoding/json"
	"github.com/simtabi/browser-foreman/src/internal/store"
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

// Another site on this computer (same-site: a dev server on localhost:3000) cannot load the page's pictures to learn
// which sites are bookmarked: they need the page's sign-in, or a token.
func TestPicturesNeedTheSignIn(t *testing.T) {
	s := withUI(seeded(t))
	h := s.Handler()
	for _, path := range []string{"/favicons/0123456789abcdef.png", "/api/v1/browsers/icon?name=Google%20Chrome"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = host
		r.Header.Set("Sec-Fetch-Site", "same-site")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Errorf("%s from another site on this computer: %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/browsers/icon?name=Google%20Chrome", nil)
	r.Host = host
	r.AddCookie(signedIn(t, s))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("the signed-in page: %d", w.Code)
	}
}

// Five wrong passwords from one place lock that place out, not the person: the owner still signs in from elsewhere.
func TestFailingWithTheOwnersNameDoesNotLockTheOwnerOut(t *testing.T) {
	s := accountsServer(t)
	p := pw(t)
	addUser(t, s, "owner", store.RoleOwner, p)
	h := s.Handler()
	try := func(addr, password string) int {
		r := httptest.NewRequest("POST", "/api/v1/auth/signin", strings.NewReader(`{"username":"owner","password":"`+password+`"}`))
		r.Host, r.RemoteAddr = host, addr
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for range 6 {
		try("198.51.100.9:4000", rand.Text())
	}
	if c := try("198.51.100.9:4000", p); c != 429 {
		t.Fatalf("the guessing address is not locked out: %d", c)
	}
	if c := try("127.0.0.1:4000", p); c != 200 {
		t.Fatalf("the owner was locked out by someone else's guesses: %d", c)
	}
}

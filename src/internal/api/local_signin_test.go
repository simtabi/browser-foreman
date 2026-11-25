package api

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// signedIn opens a one-time link and returns the cookie it set, as a browser opened by serve would hold.
func signedIn(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	link := s.LocalLink()
	w := get(s.Handler(), link[strings.Index(link, "/open/"):], nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("open %s: %d %s", link, w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == LocalCookie {
			return c
		}
	}
	t.Fatal("the link set no cookie")
	return nil
}

// Another program or person on this computer can read GET /, but gets no token from it: only a browser that opened
// a one-time link does.
func TestTheLocalPageNeedsAOneTimeLink(t *testing.T) {
	s := withUI(seeded(t))
	h := s.Handler()
	if w := get(h, "/", nil); w.Code != http.StatusUnauthorized || strings.Contains(w.Body.String(), `"token"`) || !strings.Contains(w.Body.String(), "browser-foreman open") {
		t.Fatalf("GET / without the cookie: %d %s", w.Code, w.Body)
	}
	link := s.LocalLink()
	path := link[strings.Index(link, "/open/"):]
	w := get(h, path, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("the link: %d %v", w.Code, w.Header())
	}
	var c *http.Cookie
	for _, x := range w.Result().Cookies() {
		if x.Name == LocalCookie {
			c = x
		}
	}
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie %+v", c)
	}
	if w := get(h, "/", c); w.Code != 200 || !strings.Contains(w.Body.String(), `"token":"`+apiTok+`"`) {
		t.Fatalf("with the cookie: %d", w.Code)
	}
	if w := get(h, path, nil); w.Code != http.StatusForbidden {
		t.Fatalf("a link worked twice: %d", w.Code)
	}
	// A cookie made up, or signed for another token, is refused.
	forged := &http.Cookie{Name: LocalCookie, Value: strings.Replace(c.Value, c.Value[len(c.Value)-4:], "AAAA", 1)}
	if w := get(h, "/", forged); w.Code != http.StatusUnauthorized {
		t.Fatalf("a forged cookie: %d", w.Code)
	}
	s.Cfg.APIToken = rand.Text()
	if w := get(s.Handler(), "/", c); w.Code != http.StatusUnauthorized {
		t.Fatalf("a new token must end the old sign-ins: %d", w.Code)
	}
}

// A link waits two minutes at most, and a sign-in thirty days.
func TestLinksAndSignInsExpire(t *testing.T) {
	s := withUI(seeded(t))
	now := time.Now()
	s.Now = func() time.Time { return now }
	link := s.LocalLink()
	now = now.Add(linkTTL + time.Second)
	if w := get(s.Handler(), link[strings.Index(link, "/open/"):], nil); w.Code != http.StatusForbidden {
		t.Fatalf("an old link: %d", w.Code)
	}
	c := signedIn(t, s)
	now = now.Add(localSessionTTL + time.Minute)
	if w := get(s.Handler(), "/", c); w.Code != http.StatusUnauthorized {
		t.Fatalf("an old sign-in: %d", w.Code)
	}
}

// `browser-foreman open` asks for a link with the token from .env; without it, no link.
func TestOnlyTheOwnerMayAskForALink(t *testing.T) {
	s := withUI(seeded(t))
	if w := send(s.Handler(), "POST", "/api/v1/local/links", ""); w.Code != 201 || !strings.Contains(w.Body.String(), "/open/") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("POST", "/api/v1/local/links", nil)
	r.Host = host
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", w.Code)
	}
}

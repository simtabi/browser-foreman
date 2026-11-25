package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Local mode has one owner and no sign-in, but the page still must not hand the owner's token to whatever asks for it:
// any program on this computer, or another person signed in to it, could read GET / and act as the owner. The page
// carries the token only for a browser holding the local cookie, and a browser gets that cookie by opening a one-time
// link, which only serve (opening the page itself) or `browser-foreman open` (asking with the token from .env) make.

// LocalCookie is the cookie a browser opened through a one-time link carries in local mode.
const LocalCookie = "brofo_local"

const (
	linkTTL         = 2 * time.Minute     // how long a one-time link may wait to be opened
	localSessionTTL = 30 * 24 * time.Hour // how long the browser stays signed in
)

type localLinks struct {
	mu    sync.Mutex
	codes map[string]time.Time // code → expiry; used codes are removed
}

// LocalLink makes a one-time link to the page: it works once, within two minutes.
func (s *Server) LocalLink() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	code := base64.RawURLEncoding.EncodeToString(b)
	s.links.mu.Lock()
	if s.links.codes == nil {
		s.links.codes = map[string]time.Time{}
	}
	now := s.Now()
	for c, exp := range s.links.codes {
		if now.After(exp) {
			delete(s.links.codes, c)
		}
	}
	s.links.codes[code] = now.Add(linkTTL)
	s.links.mu.Unlock()
	scheme := "http"
	if s.Cfg.Server.TLSCert != "" {
		scheme = "https"
	}
	return scheme + "://" + s.Cfg.Server.Addr + "/open/" + code
}

// useLink spends a code: true once, for a code made in the last two minutes.
func (s *Server) useLink(code string) bool {
	s.links.mu.Lock()
	defer s.links.mu.Unlock()
	exp, ok := s.links.codes[code]
	delete(s.links.codes, code)
	return ok && !s.Now().After(exp)
}

// localMAC signs a session's expiry and id with the API token, so the cookie needs no storage, outlives restarts,
// and stops working the moment the token changes.
func (s *Server) localMAC(exp, id string) string {
	m := hmac.New(sha256.New, []byte(s.Cfg.APIToken))
	m.Write([]byte("brofo-local-session|" + exp + "|" + id))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) localCookieValue() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	id := base64.RawURLEncoding.EncodeToString(b)
	exp := strconv.FormatInt(s.Now().Add(localSessionTTL).Unix(), 10)
	return exp + "." + id + "." + s.localMAC(exp, id)
}

// localSignedIn reports whether the browser holds a valid local cookie.
func (s *Server) localSignedIn(r *http.Request) bool {
	if s.Cfg.APIToken == "" {
		return false
	}
	c, err := r.Cookie(LocalCookie)
	if err != nil {
		return false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || s.Now().After(time.Unix(exp, 0)) {
		return false
	}
	return hmac.Equal([]byte(parts[2]), []byte(s.localMAC(parts[0], parts[1])))
}

// openLink is GET /open/{code}: a fresh one-time link signs this browser in and goes to the page.
func (s *Server) openLink(w http.ResponseWriter, r *http.Request) {
	if !s.useLink(r.PathValue("code")) {
		s.openFromHere(w, http.StatusForbidden, newNonce(), "That link was already used, or it is more than two minutes old.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: LocalCookie, Value: s.localCookieValue(), Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: s.secure(r), MaxAge: int(localSessionTTL.Seconds())})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// createLocalLink is POST /api/v1/local/links: `browser-foreman open` asks for a link with the token from .env.
func (s *Server) createLocalLink(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 201, map[string]any{"url": s.LocalLink(), "expires_in": int(linkTTL.Seconds())})
}

// openFromHere is the page a browser without the local cookie gets: how to open Browser Foreman.
func (s *Server) openFromHere(w http.ResponseWriter, code int, nonce, why string) {
	plainPage(w, code, nonce, "Open Browser Foreman", `<h1 class="h2">Open Browser Foreman from this computer</h1>
<p>`+why+` Browser Foreman opens only through a link it makes, so no other program or person on this computer can
act as you.</p>
<p>Run this in a terminal; it opens the page in your browser:</p>
<pre class="p-2 bg-light rounded">browser-foreman open</pre>
<p class="text-secondary">Starting it with <code>browser-foreman serve</code> opens it too.</p>`)
}

package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// SessionCookie is the cookie a signed-in browser carries. Only its SHA-256 is stored.
const SessionCookie = "brofo_session"

// Principal is who a request comes from.
type Principal struct {
	User string `json:"name"`
	Role string `json:"role"`
	Via  string `json:"via"` // token, session, or local (the configured token in local mode)
	csrf string
	sess string // the session's stored hash, for a session
}

// who works out the caller: an API token (a person's, or in local mode the configured one, which acts as the owner),
// or a session cookie. A disabled person's token or session is refused.
func (s *Server) who(r *http.Request) (Principal, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok := strings.TrimPrefix(h, "Bearer ")
		if s.Cfg.Server.Mode == config.ModeLocal && s.Cfg.APIToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(s.Cfg.APIToken)) == 1 {
			return Principal{User: "local", Role: store.RoleOwner, Via: "local"}, true
		}
		t, err := s.Data.TokenByHash(r.Context(), auth.HashSecret(tok))
		if err != nil {
			return Principal{}, false
		}
		u, err := s.Data.User(r.Context(), t.User)
		if err != nil || u.Disabled {
			return Principal{}, false
		}
		_ = s.Data.TouchToken(r.Context(), t.ID, s.Now())
		return Principal{User: u.Name, Role: u.Role, Via: "token"}, true
	}
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		x, err := s.Data.SessionByHash(r.Context(), auth.HashSecret(c.Value), s.Now())
		if err != nil {
			return Principal{}, false
		}
		u, err := s.Data.User(r.Context(), x.User)
		if err != nil || u.Disabled {
			return Principal{}, false
		}
		return Principal{User: u.Name, Role: u.Role, Via: "session", csrf: x.CSRF, sess: x.Hash}, true
	}
	return Principal{}, false
}

type ctxKey struct{}

func principal(r *http.Request) Principal { p, _ := r.Context().Value(ctxKey{}).(Principal); return p }

// require lets a request through only from a caller with at least role. A change made with a session cookie must also
// carry that session's CSRF token in X-CSRF-Token, so another site cannot make the browser send it.
func (s *Server) require(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.who(r)
		if !ok {
			if s.Cfg.Server.Mode == config.ModeLocal && s.Cfg.APIToken == "" {
				writeErr(w, http.StatusServiceUnavailable, "no API token is set: run `browser-foreman config init` or set BROFO_API_TOKEN")
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="browser-foreman"`)
			writeErr(w, http.StatusUnauthorized, "sign in, or send a valid API token")
			return
		}
		if p.Via == "session" && r.Method != http.MethodGet && r.Method != http.MethodHead &&
			subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(p.csrf)) != 1 {
			writeErr(w, http.StatusForbidden, "missing or wrong X-CSRF-Token")
			return
		}
		if store.RoleRank(p.Role) < store.RoleRank(role) {
			writeErr(w, http.StatusForbidden, "this needs the "+role+" role")
			return
		}
		next(w, r.WithContext(contextWith(r, p)))
	}
}

func clientKey(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON body: "+err.Error())
		return false
	}
	return true
}

// signIn checks a name and password and starts a session. Failures are counted per name and per address; too many
// lock both out for a while (429 with Retry-After). A missing name costs the same time as a wrong password.
func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	now := s.Now()
	keys := []string{"user:" + strings.ToLower(in.Username), "addr:" + clientKey(r)}
	for _, k := range keys {
		if ok, until := s.Limit.Allowed(k, now); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(until.Sub(now).Seconds())+1))
			writeErr(w, http.StatusTooManyRequests, "too many failed sign-ins; try again later")
			return
		}
	}
	u, err := s.Data.User(r.Context(), in.Username)
	hash := ""
	if err == nil && !u.Disabled {
		hash = u.PasswordHash
	}
	if !auth.VerifyOrWaste(hash, in.Password) {
		for _, k := range keys {
			s.Limit.Fail(k, now)
		}
		writeErr(w, http.StatusUnauthorized, "wrong name or password")
		return
	}
	for _, k := range keys {
		s.Limit.Succeed(k)
	}
	sid, err1 := auth.NewSecret("")
	csrf, err2 := auth.NewSecret("")
	if err1 != nil || err2 != nil {
		writeErr(w, 500, "could not make a session")
		return
	}
	ttl := s.Cfg.Auth.SessionTTL.Duration
	if err := s.Data.CreateSession(r.Context(), store.Session{Hash: auth.HashSecret(sid), User: u.Name, CSRF: csrf, Created: now, Expires: now.Add(ttl)}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: sid, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: r.TLS != nil, MaxAge: int(ttl.Seconds())})
	_, _ = s.Bus.Publish("auth.signed_in", map[string]string{"user": u.Name})
	writeJSON(w, 200, map[string]any{"user": Principal{User: u.Name, Role: u.Role, Via: "session"}, "csrf": csrf})
}

func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = s.Data.DeleteSession(r.Context(), auth.HashSecret(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	out := map[string]any{"user": p}
	if p.Via == "session" {
		out["csrf"] = p.csrf
	}
	writeJSON(w, 200, out)
}

// ---- tokens ----

type tokenOut struct {
	ID       string    `json:"id"`
	User     string    `json:"user"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used,omitempty"`
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	who := p.User
	if r.URL.Query().Get("all") == "1" && p.Role == store.RoleOwner {
		who = ""
	}
	all, err := s.Data.Tokens(r.Context(), who)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := []tokenOut{}
	for _, t := range all {
		out = append(out, tokenOut{t.ID, t.User, t.Name, t.Created, t.LastUsed})
	}
	writeJSON(w, 200, map[string]any{"tokens": out})
}

// createToken makes an API token for the caller. The token is in this answer only; afterwards just its hash exists.
func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Via == "local" {
		writeErr(w, 400, "the configured token is not a person: sign in, or use `browser-foreman token create --user NAME`")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 {
		writeErr(w, 400, "give the token a name (what uses it), up to 100 characters")
		return
	}
	secret, err := auth.NewSecret("brofo_")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	t := store.Token{ID: "tk_" + newID()[3:], User: p.User, Name: strings.TrimSpace(in.Name), Hash: auth.HashSecret(secret), Created: s.Now().UTC()}
	if err := s.Data.CreateToken(r.Context(), t); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"token": tokenOut{t.ID, t.User, t.Name, t.Created, time.Time{}}, "secret": secret,
		"note": "Copy the secret now: it is not shown again."})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	all, err := s.Data.Tokens(r.Context(), "")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, t := range all {
		if t.ID != r.PathValue("id") {
			continue
		}
		if t.User != p.User && p.Role != store.RoleOwner {
			break // someone else's token: answer as if it did not exist
		}
		if err := s.Data.RevokeToken(r.Context(), t.ID); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeErr(w, 404, "no token with that id")
}

// ---- users (owner only) ----

type userOut struct {
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	Disabled bool      `json:"disabled"`
	Created  time.Time `json:"created"`
}

func out(u store.User) userOut { return userOut{u.Name, u.Role, u.Disabled, u.Created} }

func validRole(r string) bool { return store.RoleRank(r) > 0 }

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	all, err := s.Data.Users(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	res := []userOut{}
	for _, u := range all {
		res = append(res, out(u))
	}
	writeJSON(w, 200, map[string]any{"users": res})
}

func (s *Server) addUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validRole(in.Role) {
		writeErr(w, 400, "role must be viewer, editor or owner")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u := store.User{Name: in.Name, Role: in.Role, PasswordHash: hash, Created: s.Now().UTC()}
	if err := s.Data.CreateUser(r.Context(), u); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("user.created", map[string]string{"user": u.Name, "role": u.Role})
	writeJSON(w, 201, out(u))
}

// activeOwners counts owners who can still sign in, so the last one is never removed, demoted or disabled.
func (s *Server) activeOwners(r *http.Request) (int, error) {
	all, err := s.Data.Users(r.Context())
	n := 0
	for _, u := range all {
		if u.Role == store.RoleOwner && !u.Disabled {
			n++
		}
	}
	return n, err
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role     *string `json:"role"`
		Password *string `json:"password"`
		Disabled *bool   `json:"disabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	u, err := s.Data.User(r.Context(), r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no user with that name")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	stillOwner := u.Role == store.RoleOwner && !u.Disabled
	if in.Role != nil {
		if !validRole(*in.Role) {
			writeErr(w, 400, "role must be viewer, editor or owner")
			return
		}
		u.Role = *in.Role
	}
	if in.Disabled != nil {
		u.Disabled = *in.Disabled
	}
	if in.Password != nil {
		if u.PasswordHash, err = auth.HashPassword(*in.Password); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}
	if stillOwner && (u.Role != store.RoleOwner || u.Disabled) {
		if n, _ := s.activeOwners(r); n <= 1 {
			writeErr(w, 409, "this is the last owner who can sign in; make another owner first")
			return
		}
	}
	if err := s.Data.UpdateUser(r.Context(), u); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if in.Password != nil {
		// A new password signs the person out everywhere else; the caller's own session stays when it is theirs.
		keep := ""
		if p := principal(r); p.User == u.Name && p.Via == "session" {
			keep = p.sess
		}
		if err := s.Data.DeleteSessions(r.Context(), u.Name, keep); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, 200, out(u))
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.Data.User(r.Context(), r.PathValue("name"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no user with that name")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if u.Role == store.RoleOwner && !u.Disabled {
		if n, _ := s.activeOwners(r); n <= 1 {
			writeErr(w, 409, "this is the last owner who can sign in; make another owner first")
			return
		}
	}
	if err := s.Data.DeleteUser(r.Context(), u.Name); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, _ = s.Bus.Publish("user.deleted", map[string]string{"user": u.Name})
	w.WriteHeader(http.StatusNoContent)
}

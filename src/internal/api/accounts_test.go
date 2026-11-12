package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

const host = "127.0.0.1:8765"

// pw makes a password at run time; none is written in the source.
func pw(t *testing.T) string {
	t.Helper()
	s, err := auth.NewSecret("")
	if err != nil {
		t.Fatal(err)
	}
	return s[:24]
}

func accountsServer(t *testing.T) *Server {
	t.Helper()
	s := newServer(t, "tok")
	s.Cfg.Server.Mode = config.ModeAccounts
	return s
}

func addUser(t *testing.T, s *Server, name, role, password string) {
	t.Helper()
	h, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Data.CreateUser(context.Background(), store.User{Name: name, Role: role, PasswordHash: h, Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

type session struct {
	cookie *http.Cookie
	csrf   string
}

func signIn(t *testing.T, h http.Handler, name, password string) (*httptest.ResponseRecorder, session) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": name, "password": password})
	w := do(h, "POST", "/api/v1/auth/signin", string(body), host, "", "")
	var out struct {
		CSRF string `json:"csrf"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	var c *http.Cookie
	for _, x := range w.Result().Cookies() {
		if x.Name == SessionCookie {
			c = x
		}
	}
	return w, session{c, out.CSRF}
}

func (x session) do(h http.Handler, method, url, body string, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.Host = host
	r.AddCookie(x.cookie)
	if csrf {
		r.Header.Set("X-CSRF-Token", x.csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSignInStartsAHardenedSession(t *testing.T) {
	s := accountsServer(t)
	p := pw(t)
	addUser(t, s, "ada", store.RoleOwner, p)
	h := s.Handler()
	if w, _ := signIn(t, h, "ada", p+"x"); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	if w, _ := signIn(t, h, "nobody", p); w.Code != 401 {
		t.Fatalf("unknown user: %d", w.Code)
	}
	w, sess := signIn(t, h, "ada", p)
	if w.Code != 200 || sess.cookie == nil || sess.csrf == "" {
		t.Fatalf("sign in %d %s", w.Code, w.Body)
	}
	if !sess.cookie.HttpOnly || sess.cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie flags %+v", sess.cookie)
	}
	if strings.Contains(w.Body.String(), sess.cookie.Value) {
		t.Fatal("the session id must only travel in the cookie")
	}
	if w := sess.do(h, "GET", "/api/v1/auth/me", "", false); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"ada"`) {
		t.Fatalf("me %d %s", w.Code, w.Body)
	}
	if w := sess.do(h, "POST", "/api/v1/auth/signout", "", false); w.Code != 204 {
		t.Fatalf("sign out %d", w.Code)
	}
	if w := sess.do(h, "GET", "/api/v1/auth/me", "", false); w.Code != 401 {
		t.Fatalf("session still works after sign-out: %d", w.Code)
	}
}

func TestSignInIsRateLimited(t *testing.T) {
	s := accountsServer(t)
	p := pw(t)
	addUser(t, s, "ada", store.RoleOwner, p)
	h := s.Handler()
	for i := 0; i < 5; i++ {
		signIn(t, h, "ada", "wrong-"+p)
	}
	w, _ := signIn(t, h, "ada", p)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("the right password after 5 failures must still be refused for a while: %d", w.Code)
	}
}

func TestSessionWritesNeedTheCSRFToken(t *testing.T) {
	s := accountsServer(t)
	p := pw(t)
	addUser(t, s, "ada", store.RoleOwner, p)
	h := s.Handler()
	_, sess := signIn(t, h, "ada", p)
	if w := sess.do(h, "POST", "/api/v1/tokens", `{"name":"cli"}`, false); w.Code != 403 {
		t.Fatalf("write without CSRF: %d", w.Code)
	}
	if w := sess.do(h, "POST", "/api/v1/tokens", `{"name":"cli"}`, true); w.Code != 201 {
		t.Fatalf("write with CSRF: %d %s", w.Code, w.Body)
	}
}

func TestRolesAndTokens(t *testing.T) {
	s := accountsServer(t)
	po, pv := pw(t), pw(t)
	addUser(t, s, "ada", store.RoleOwner, po)
	addUser(t, s, "bob", store.RoleViewer, pv)
	h := s.Handler()

	if w := do(h, "GET", "/api/v1/workspaces", "", host, "", "tok"); w.Code != 401 {
		t.Fatalf("accounts mode must refuse the config token: %d", w.Code)
	}

	_, bob := signIn(t, h, "bob", pv)
	if w := bob.do(h, "GET", "/api/v1/workspaces", "", false); w.Code != 200 {
		t.Fatalf("viewer read: %d", w.Code)
	}
	if w := bob.do(h, "GET", "/api/v1/users", "", false); w.Code != 403 {
		t.Fatalf("viewer listed users: %d", w.Code)
	}
	w := bob.do(h, "POST", "/api/v1/tokens", `{"name":"script"}`, true)
	var made struct {
		Token  struct{ ID string } `json:"token"`
		Secret string              `json:"secret"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &made)
	if w.Code != 201 || !strings.HasPrefix(made.Secret, "brofo_") {
		t.Fatalf("token create %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/v1/auth/me", "", host, "", made.Secret); w.Code != 200 || !strings.Contains(w.Body.String(), `"via":"token"`) {
		t.Fatalf("token use %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/api/v1/tokens", "", host, "", made.Secret); strings.Contains(w.Body.String(), made.Secret) {
		t.Fatal("the secret is listed")
	}

	_, ada := signIn(t, h, "ada", po)
	if w := ada.do(h, "PATCH", "/api/v1/users/bob", `{"disabled":true}`, true); w.Code != 200 {
		t.Fatalf("disable %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/auth/me", "", host, "", made.Secret); w.Code != 401 {
		t.Fatalf("a disabled user's token still works: %d", w.Code)
	}
	if w := bob.do(h, "GET", "/api/v1/auth/me", "", false); w.Code != 401 {
		t.Fatalf("a disabled user's session still works: %d", w.Code)
	}
	if w, _ := signIn(t, h, "bob", pv); w.Code != 401 {
		t.Fatalf("a disabled user signed in: %d", w.Code)
	}
	if w := ada.do(h, "PATCH", "/api/v1/users/bob", `{"disabled":false}`, true); w.Code != 200 {
		t.Fatal("enable")
	}
	if w := ada.do(h, "DELETE", "/api/v1/tokens/"+made.Token.ID, "", true); w.Code != 204 {
		t.Fatalf("owner revokes another's token: %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/auth/me", "", host, "", made.Secret); w.Code != 401 {
		t.Fatalf("revoked token still works: %d", w.Code)
	}
}

func TestTheLastOwnerStays(t *testing.T) {
	s := accountsServer(t)
	p := pw(t)
	addUser(t, s, "ada", store.RoleOwner, p)
	h := s.Handler()
	_, ada := signIn(t, h, "ada", p)
	for _, c := range []struct{ method, body string }{
		{"PATCH", `{"role":"editor"}`}, {"PATCH", `{"disabled":true}`}, {"DELETE", ""},
	} {
		if w := ada.do(h, c.method, "/api/v1/users/ada", c.body, true); w.Code != 409 {
			t.Fatalf("%s %s on the last owner: %d", c.method, c.body, w.Code)
		}
	}
	body, _ := json.Marshal(map[string]string{"name": "cy", "role": "owner", "password": pw(t)})
	if w := ada.do(h, "POST", "/api/v1/users", string(body), true); w.Code != 201 || strings.Contains(w.Body.String(), "argon2") {
		t.Fatalf("add owner %d %s", w.Code, w.Body)
	}
	if w := ada.do(h, "PATCH", "/api/v1/users/ada", `{"role":"editor"}`, true); w.Code != 200 {
		t.Fatalf("demote with another owner: %d", w.Code)
	}
}

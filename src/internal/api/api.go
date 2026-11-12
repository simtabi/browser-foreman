// Package api is Browser Foreman's HTTP server: the REST API under /api/v1, the event stream, and (from phase 4 of
// the roadmap) the web interface. Every route and the role it needs is declared in Handler; docs/tools/rest-api.md
// lists them.
//
// Who is asking: a person's API token (Authorization: Bearer brofo_…), a signed-in browser (the brofo_session cookie,
// with X-CSRF-Token on every change), or in local mode the configured BROFO_API_TOKEN, which acts as the owner. Every
// request must also come to a local Host (local mode) and, from a browser, from this server's own Origin.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/events"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/version"
	"github.com/simtabi/browser-foreman/src/internal/webhooks"
)

// Server holds what the handlers need.
type Server struct {
	Cfg      config.Config
	Bus      *events.Bus
	Hooks    *webhooks.Store
	Dispatch *webhooks.Dispatcher
	Data     store.Store
	Limit    *auth.Limiter // failed sign-ins
	Now      func() time.Time
}

func contextWith(r *http.Request, p Principal) context.Context {
	return context.WithValue(r.Context(), ctxKey{}, p)
}

// Handler is the whole HTTP surface, wrapped in the security checks.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/health", s.health)
	m.HandleFunc("GET /api/v1/version", s.versionInfo)
	// Each route names the least role it needs (store.RoleViewer < RoleEditor < RoleOwner).
	m.HandleFunc("POST /api/v1/auth/signin", s.signIn)
	m.HandleFunc("POST /api/v1/auth/signout", s.signOut)
	m.HandleFunc("GET /api/v1/auth/me", s.require(store.RoleViewer, s.me))
	m.HandleFunc("GET /api/v1/events", s.require(store.RoleViewer, s.stream))
	m.HandleFunc("GET /api/v1/tokens", s.require(store.RoleViewer, s.listTokens))
	m.HandleFunc("POST /api/v1/tokens", s.require(store.RoleViewer, s.createToken))
	m.HandleFunc("DELETE /api/v1/tokens/{id}", s.require(store.RoleViewer, s.revokeToken))
	m.HandleFunc("GET /api/v1/users", s.require(store.RoleOwner, s.listUsers))
	m.HandleFunc("POST /api/v1/users", s.require(store.RoleOwner, s.addUser))
	m.HandleFunc("PATCH /api/v1/users/{name}", s.require(store.RoleOwner, s.updateUser))
	m.HandleFunc("DELETE /api/v1/users/{name}", s.require(store.RoleOwner, s.deleteUser))
	m.HandleFunc("GET /api/v1/webhooks", s.require(store.RoleOwner, s.listHooks))
	m.HandleFunc("POST /api/v1/webhooks", s.require(store.RoleOwner, s.addHook))
	m.HandleFunc("DELETE /api/v1/webhooks/{id}", s.require(store.RoleOwner, s.removeHook))
	m.HandleFunc("POST /api/v1/webhooks/{id}/test", s.require(store.RoleOwner, s.testHook))
	m.HandleFunc("GET /api/v1/workspaces", s.require(store.RoleViewer, s.listWorkspaces))
	m.HandleFunc("GET /api/v1/workspaces/{id}", s.require(store.RoleViewer, s.getWorkspace))
	m.HandleFunc("GET /api/v1/workspaces/{id}/history", s.require(store.RoleViewer, s.getHistory))
	m.HandleFunc("GET /{$}", s.home)
	return s.guard(m)
}

// guard refuses requests aimed at this server through another name (DNS rebinding) and cross-site browser requests,
// and sets the security headers every response carries.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		if s.Cfg.Server.Mode == config.ModeLocal && !localHost(r.Host) {
			writeErr(w, http.StatusForbidden, "this server answers on localhost only")
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host && o != "https://"+r.Host {
			writeErr(w, http.StatusForbidden, "requests from other sites are refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) versionInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date})
}

func (s *Server) home(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Browser Foreman</title>
<body style="font:16px system-ui;margin:3rem;max-width:40rem"><h1>Browser Foreman</h1>
<p>The server is running. API: <code>/api/v1</code> · events: <code>/api/v1/events</code>.</p>
<p>The web interface is served here once it is ported (see docs/roadmap.md).</p></body>`))
}

// stream sends events as Server-Sent Events: first anything after Last-Event-ID still kept, then live ones, with a
// comment line every 15 seconds so proxies keep the connection open.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming is not supported here")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, stop := s.Bus.Subscribe(64)
	defer stop()
	if last, err := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64); err == nil {
		missed, lost := s.Bus.Since(last)
		if lost {
			_, _ = w.Write([]byte(": some events were missed; reload the state\n\n"))
		}
		for _, e := range missed {
			_, _ = w.Write(e.SSE())
		}
	}
	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			_, _ = w.Write(e.SSE())
			fl.Flush()
		case <-tick.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			fl.Flush()
		}
	}
}

type hookOut struct {
	ID      string    `json:"id"`
	URL     string    `json:"url"`
	Events  []string  `json:"events"`
	Created time.Time `json:"created"`
	Active  bool      `json:"active"`
}

func redact(x webhooks.Subscription) hookOut {
	return hookOut{ID: x.ID, URL: x.URL, Events: x.Events, Created: x.Created, Active: x.Active}
}

func (s *Server) listHooks(w http.ResponseWriter, _ *http.Request) {
	all, err := s.Hooks.List()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := []hookOut{}
	for _, x := range all {
		out = append(out, redact(x))
	}
	writeJSON(w, 200, map[string]any{"webhooks": out})
}

func (s *Server) addHook(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string   `json:"url"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeErr(w, 400, "body must be {url, events, secret}: "+err.Error())
		return
	}
	sub, err := s.Hooks.Add(webhooks.Subscription{ID: newID(), URL: in.URL, Events: in.Events, Secret: in.Secret, Created: s.Now().UTC()})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("webhook.created", map[string]string{"id": sub.ID, "url": sub.URL})
	writeJSON(w, 201, redact(sub))
}

func (s *Server) removeHook(w http.ResponseWriter, r *http.Request) {
	ok, err := s.Hooks.Remove(r.PathValue("id"))
	switch {
	case err != nil:
		writeErr(w, 500, err.Error())
	case !ok:
		writeErr(w, 404, "no webhook with that id")
	default:
		_, _ = s.Bus.Publish("webhook.deleted", map[string]string{"id": r.PathValue("id")})
		w.WriteHeader(http.StatusNoContent)
	}
}

// testHook delivers a webhook.test event to one subscription now and answers with the delivery record.
func (s *Server) testHook(w http.ResponseWriter, r *http.Request) {
	all, err := s.Hooks.List()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, x := range all {
		if x.ID != r.PathValue("id") {
			continue
		}
		e := events.Event{ID: 0, Type: "webhook.test", At: s.Now().UTC(), Data: json.RawMessage(`{"hello":"from browser-foreman"}`)}
		ctx, cancel := context.WithTimeout(r.Context(), s.Cfg.Webhooks.Timeout.Duration*time.Duration(s.Cfg.Webhooks.Attempts+1))
		defer cancel()
		writeJSON(w, 200, s.Dispatch.SendOne(ctx, x, e))
		return
	}
	writeErr(w, 404, "no webhook with that id")
}

// Run serves until ctx ends, delivering every published event to the webhooks in the background.
func (s *Server) Run(ctx context.Context) error {
	ch, stop := s.Bus.Subscribe(256)
	defer stop()
	go func() {
		for e := range ch {
			s.Dispatch.Deliver(ctx, e)
		}
	}()
	srv := &http.Server{Addr: s.Cfg.Server.Addr, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	_, _ = s.Bus.Publish("server.started", map[string]string{"addr": s.Cfg.Server.Addr, "version": version.Version})
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type wsOut struct {
	store.Workspace
	Bookmarks int `json:"bookmarks"`
	Folders   int `json:"folders"`
}

func (s *Server) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	all, err := s.Data.Workspaces(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := []wsOut{}
	for _, x := range all {
		t, _, err := s.Data.State(r.Context(), x.ID, 0)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeErr(w, 500, err.Error())
			return
		}
		bm, fo := t.Count()
		out = append(out, wsOut{x, bm, fo})
	}
	writeJSON(w, 200, map[string]any{"workspaces": out})
}

func (s *Server) getWorkspace(w http.ResponseWriter, r *http.Request) {
	x, err := s.Data.Workspace(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	at := 0
	if v := r.URL.Query().Get("at"); v != "" {
		if at, err = strconv.Atoi(v); err != nil || at < 1 {
			writeErr(w, 400, "at must be an entry number (1 or more)")
			return
		}
	}
	t, e, err := s.Data.State(r.Context(), x.ID, at)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no such entry")
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	bm, fo := t.Count()
	writeJSON(w, 200, map[string]any{"workspace": wsOut{x, bm, fo}, "entry": e, "tree": t})
}

func (s *Server) getHistory(w http.ResponseWriter, r *http.Request) {
	h, err := s.Data.History(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if h == nil {
		h = []store.Entry{}
	}
	writeJSON(w, 200, map[string]any{"history": h})
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "wh_" + hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

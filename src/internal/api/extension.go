package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/tabs"
)

// An extension pairs with a one-time code the owner makes (here, or with `browser-foreman pair`), and gets its own
// API token named "extension:<browser>", which the token list shows and revoke removes. In local mode the token
// belongs to the built-in "local" account, which has no password and so can never sign in.

const (
	extPrefix   = "extension:"
	pairTTL     = 10 * time.Minute
	codeLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O or 1/I: read off a screen and typed
)

type pairing struct {
	user    string
	expires time.Time
}

// extState is the server's memory of extensions: open pairing codes and queued commands.
type extState struct {
	mu       sync.Mutex
	pairings map[string]pairing
	commands map[string][]*command // by browser
	byID     map[string]*command
	wake     chan struct{}
}

// command is something the server asks an extension to do in its browser.
type command struct {
	ID        string    `json:"id"`
	Browser   string    `json:"browser"`
	Kind      string    `json:"kind"` // open, open-group
	URLs      []string  `json:"urls"`
	Title     string    `json:"title,omitempty"`
	Color     string    `json:"color,omitempty"`
	Incognito bool      `json:"incognito,omitempty"`
	State     string    `json:"state"` // queued, sent, done, failed
	Error     string    `json:"error,omitempty"`
	Created   time.Time `json:"created"`
}

func (e *extState) init() {
	if e.pairings == nil {
		e.pairings, e.commands, e.byID, e.wake = map[string]pairing{}, map[string][]*command{}, map[string]*command{}, make(chan struct{})
	}
}

func newCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	out := make([]byte, 0, 9)
	for i, x := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, codeLetters[int(x)%len(codeLetters)])
	}
	return string(out)
}

// createPairing makes a one-time code, good for ten minutes.
func (s *Server) createPairing(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	user := p.User
	if p.Via == "local" {
		if err := s.localAccount(r.Context()); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	code := newCode()
	s.ext.mu.Lock()
	s.ext.init()
	now := s.Now()
	for c, x := range s.ext.pairings { // forget expired codes
		if now.After(x.expires) {
			delete(s.ext.pairings, c)
		}
	}
	s.ext.pairings[code] = pairing{user: user, expires: now.Add(pairTTL)}
	s.ext.mu.Unlock()
	writeJSON(w, 201, map[string]any{"code": code, "expires": now.Add(pairTTL)})
}

// localAccount makes sure the "local" account exists for local mode's extension tokens. It has no password hash, so
// signing in as it always fails.
func (s *Server) localAccount(ctx context.Context) error {
	if _, err := s.Data.User(ctx, "local"); err == nil {
		return nil
	}
	return s.Data.CreateUser(ctx, store.User{Name: "local", Role: store.RoleOwner, Created: s.Now().UTC()})
}

// pair trades a pairing code for an extension token. The code works once, for ten minutes; failures count against
// the sign-in limiter, so codes cannot be guessed.
func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code    string `json:"code"`
		Browser string `json:"browser"`
	}
	if !decode(w, r, &in) {
		return
	}
	now := s.Now()
	key := "pair:" + clientKey(r)
	if ok, until := s.Limit.Allowed(key, now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(until.Sub(now).Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many wrong codes; try again later")
		return
	}
	in.Browser = strings.TrimSpace(in.Browser)
	if in.Browser == "" || len(in.Browser) > 60 {
		writeErr(w, 400, "browser: 1 to 60 characters")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	s.ext.mu.Lock()
	s.ext.init()
	p, ok := s.ext.pairings[code]
	if ok {
		delete(s.ext.pairings, code) // one use, whatever happens next
	}
	s.ext.mu.Unlock()
	if !ok || now.After(p.expires) {
		s.Limit.Fail(key, now)
		writeErr(w, 401, "that code is wrong or has expired; make a new one")
		return
	}
	s.Limit.Succeed(key)
	secret, err := auth.NewSecret("brofo_")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	t := store.Token{ID: "tk_" + newID()[3:], User: p.user, Name: extPrefix + in.Browser, Hash: auth.HashSecret(secret), Created: now.UTC()}
	if err := s.Data.CreateToken(r.Context(), t); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	_, _ = s.Bus.Publish("extension.paired", map[string]any{"browser": in.Browser, "token": t.ID})
	writeJSON(w, 201, map[string]any{"token": secret, "id": t.ID, "browser": in.Browser})
}

// requireExtension lets through only an extension's own token, and hands the handler its browser.
func (s *Server) requireExtension(next func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := s.who(r)
		if !ok || p.Via != "token" || !strings.HasPrefix(p.tokenName, extPrefix) {
			writeErr(w, 401, "send a paired extension's token")
			return
		}
		next(w, r.WithContext(contextWith(r, p)), strings.TrimPrefix(p.tokenName, extPrefix))
	}
}

// pushTabs takes what the extension has open: windows, tabs and tab groups.
func (s *Server) pushTabs(w http.ResponseWriter, r *http.Request, browser string) {
	var in struct {
		Windows []struct {
			ID        tabs.ID `json:"id"`
			Incognito bool    `json:"incognito"`
			Tabs      []struct {
				ID     tabs.ID `json:"id"`
				URL    string  `json:"url"`
				Title  string  `json:"title"`
				Active bool    `json:"active"`
				Pinned bool    `json:"pinned"`
				Group  tabs.ID `json:"groupId"`
			} `json:"tabs"`
		} `json:"windows"`
		Groups []struct {
			ID        tabs.ID `json:"id"`
			WindowID  tabs.ID `json:"windowId"`
			Title     string  `json:"title"`
			Color     string  `json:"color"`
			Collapsed bool    `json:"collapsed"`
		} `json:"groups"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Windows) > 200 {
		writeErr(w, 400, "at most 200 windows")
		return
	}
	b := tabs.Browser{Browser: browser}
	for i, win := range in.Windows {
		x := tabs.Window{Index: i + 1, ID: win.ID, Mode: "normal"}
		if win.Incognito {
			x.Mode = "incognito"
		}
		if len(win.Tabs) > 2000 {
			writeErr(w, 400, "at most 2000 tabs a window")
			return
		}
		for k, t := range win.Tabs {
			if t.Active {
				x.Active = k + 1
			}
			group := t.Group
			if group == "-1" {
				group = ""
			}
			x.Tabs = append(x.Tabs, tabs.Tab{URL: t.URL, Title: t.Title, ID: t.ID, Pinned: t.Pinned, Group: group})
		}
		for _, g := range in.Groups {
			if g.WindowID == win.ID {
				x.Groups = append(x.Groups, tabs.Group{ID: g.ID, Title: g.Title, Color: g.Color, Collapsed: g.Collapsed})
			}
		}
		b.Windows = append(b.Windows, x)
	}
	s.Tabs.Push(b)
	w.WriteHeader(http.StatusNoContent)
}

var errUnknownKind = errors.New(`kind must be "open" or "open-group"`)

// queueCommand asks a browser's extension to open links (as tabs, or as one named tab group). Owner only.
func (s *Server) queueCommand(w http.ResponseWriter, r *http.Request) {
	var in command
	if !decode(w, r, &in) {
		return
	}
	if in.Kind != "open" && in.Kind != "open-group" {
		writeErr(w, 400, errUnknownKind.Error())
		return
	}
	if err := checkWebLinks(in.URLs); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if len(in.Title) > 100 {
		writeErr(w, 400, "title: at most 100 characters")
		return
	}
	in.ID, in.State, in.Error, in.Created = "cmd_"+newID()[3:], "queued", "", s.Now().UTC()
	s.ext.mu.Lock()
	s.ext.init()
	s.ext.commands[in.Browser] = append(s.ext.commands[in.Browser], &in)
	s.ext.byID[in.ID] = &in
	close(s.ext.wake) // wake every waiting extension
	s.ext.wake = make(chan struct{})
	s.ext.mu.Unlock()
	writeJSON(w, 202, map[string]any{"command": in})
}

// nextCommands answers an extension's long poll (?wait= up to 25 seconds) with the commands queued for its browser.
func (s *Server) nextCommands(w http.ResponseWriter, r *http.Request, browser string) {
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	wait = max(0, min(wait, 25))
	deadline := time.After(time.Duration(wait) * time.Second)
	for {
		s.ext.mu.Lock()
		s.ext.init()
		var out []command
		for _, c := range s.ext.commands[browser] {
			if c.State == "queued" {
				c.State = "sent"
				out = append(out, *c)
			}
		}
		wake := s.ext.wake
		s.ext.mu.Unlock()
		if len(out) > 0 || wait == 0 {
			if out == nil {
				out = []command{}
			}
			writeJSON(w, 200, map[string]any{"commands": out})
			return
		}
		select {
		case <-wake:
		case <-deadline:
			wait = 0
		case <-r.Context().Done():
			return
		}
	}
}

// commandDone records what the extension did with a command.
func (s *Server) commandDone(w http.ResponseWriter, r *http.Request, browser string) {
	var in struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if !decode(w, r, &in) {
		return
	}
	s.ext.mu.Lock()
	defer s.ext.mu.Unlock()
	s.ext.init()
	c := s.ext.byID[r.PathValue("id")]
	if c == nil || c.Browser != browser {
		writeErr(w, 404, "no command with that id for this browser")
		return
	}
	c.State, c.Error = "done", ""
	if !in.OK {
		c.State, c.Error = "failed", in.Error
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getCommand(w http.ResponseWriter, r *http.Request) {
	s.ext.mu.Lock()
	defer s.ext.mu.Unlock()
	s.ext.init()
	c := s.ext.byID[r.PathValue("id")]
	if c == nil {
		writeErr(w, 404, "no command with that id")
		return
	}
	writeJSON(w, 200, map[string]any{"command": c})
}

// extensionStatus lists the paired extensions (their tokens; never the secrets).
func (s *Server) extensionStatus(w http.ResponseWriter, r *http.Request) {
	all, err := s.Data.Tokens(r.Context(), "")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := []tokenOut{}
	for _, t := range all {
		if strings.HasPrefix(t.Name, extPrefix) {
			out = append(out, tokenOut{t.ID, t.User, t.Name, t.Created, t.LastUsed})
		}
	}
	writeJSON(w, 200, map[string]any{"extensions": out})
}

// checkWebLinks accepts 1 to 200 http(s) links.
func checkWebLinks(urls []string) error {
	if len(urls) == 0 || len(urls) > 200 {
		return fmt.Errorf("give 1 to 200 links")
	}
	for _, u := range urls {
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return fmt.Errorf("%q: only http and https links", u)
		}
	}
	return nil
}

// readBookmarks takes the browser's whole bookmark tree, sent by its extension, into a workspace ("<browser>-live"
// unless named) as one import entry; the previous tree stays in history. Needs an editor's token.
func (s *Server) readBookmarks(w http.ResponseWriter, r *http.Request, browser string) {
	if store.RoleRank(principal(r).Role) < store.RoleRank(store.RoleEditor) {
		writeErr(w, 403, "this needs the editor role")
		return
	}
	var in struct {
		Workspace string             `json:"workspace"`
		Nodes     []importer.ExtNode `json:"nodes"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, importer.MaxSize))
	if err := dec.Decode(&in); err != nil {
		writeErr(w, 400, "bad JSON body: "+err.Error())
		return
	}
	t, err := importer.FromExtension(in.Nodes)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id := in.Workspace
	if id == "" {
		id = slug(browser) + "-live"
	}
	if err := store.ValidID(id); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	x, err := s.Data.Workspace(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		if x, err = s.Data.CreateWorkspace(r.Context(), id, browser+" (live)", s.Now()); err == nil {
			_, _ = s.Bus.Publish("workspace.created", map[string]any{"workspace": x.ID, "name": x.Name})
		}
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	bm, fo := t.Count()
	summary := fmt.Sprintf("Read %d bookmarks in %d folders from %s (extension)", bm, fo, browser)
	s.commit(w, r, x, 0, false, false, store.Change{Op: "import", Summary: summary, Tree: t}, nil, "imported")
}

// slug makes a workspace id from a browser's name: "Google Chrome" → "google-chrome".
func slug(name string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

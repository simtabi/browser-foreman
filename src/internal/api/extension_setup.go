package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Connecting an extension in one click, and helping install it.
//
// One-click pairing works like a device sign-in. The extension asks to connect (POST /extension/requests, from an
// extension's own origin, no token), and gets back a request id, a poll secret and a four-letter match code, which
// its popup shows. The owner sees the request in the web interface, with the same match code, and approves or denies
// it. The extension polls with its secret; once approved, it gets its token, once. A request lasts five minutes;
// at most five wait at once. A page or program that is not an extension cannot ask, and nothing connects unless the
// owner approves, having compared the code.
//
// Guided install: the owner can have the server open a browser's extensions page (a fixed address per browser, never
// one given in the request) and show the built extension's folder in Finder.

const (
	requestTTL   = 5 * time.Minute
	maxRequests  = 5
	matchLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ" // letters only: read off one screen and compared with another
)

// pairRequest is an extension waiting for the owner's approval.
type pairRequest struct {
	ID       string
	Browser  string
	Version  string
	Instance string             // the asking copy's own id (made once by it); "" from extensions before 0.1.7
	Where    *browsers.Instance // the running browser it asked from, where the platform can tell
	Match    string
	pollHash string
	Created  time.Time
	Expires  time.Time
	State    string // pending, approved, denied
	token    string // the new token's secret, held until the extension collects it
}

// seenExt is what an extension last reported about itself.
type seenExt struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

func newMatch() string {
	b := make([]byte, 4)
	for i := range b {
		b[i] = matchLetters[randIndex(len(matchLetters))]
	}
	return string(b)
}

// prune forgets expired requests and commands no longer worth keeping. Call with e.mu held.
func (e *extState) prune(now time.Time) {
	for id, q := range e.requests {
		if now.After(q.Expires) {
			delete(e.requests, id)
		}
	}
	for b, list := range e.commands {
		kept := list[:0]
		for _, c := range list {
			if now.Sub(c.Created) < commandTTL {
				kept = append(kept, c)
			} else {
				delete(e.byID, c.ID)
			}
		}
		if len(kept) == 0 {
			delete(e.commands, b)
		} else {
			e.commands[b] = kept
		}
	}
}

// requestPairing is an extension asking to connect. Only an extension's own origin may ask.
func (s *Server) requestPairing(w http.ResponseWriter, r *http.Request) {
	if !extensionOrigin(r.Header.Get("Origin")) {
		writeErr(w, http.StatusForbidden, "only the Browser Foreman extension can ask to connect")
		return
	}
	// The Origin header proves nothing on its own (any program can send it), so asking is for this computer only. An
	// extension on another computer pairs with a one-time code instead (browser-foreman pair).
	if !fromThisComputer(r) {
		writeErr(w, http.StatusForbidden, "asking to connect works only from this computer; elsewhere, pair with a code from browser-foreman pair")
		return
	}
	var in struct {
		Browser  string `json:"browser"`
		Version  string `json:"version"`
		Instance string `json:"instance"`
		// Replace says the owner pressed Connect in this copy (its popup, or Use this browser in the page): it may ask
		// although another copy of its browser is connected. Asking by itself, it may not, so two copies never race.
		Replace bool `json:"replace"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Browser, in.Version, in.Instance = strings.TrimSpace(in.Browser), strings.TrimSpace(in.Version), strings.TrimSpace(in.Instance)
	if in.Browser == "" || len(in.Browser) > 60 || len(in.Version) > 30 || len(in.Instance) > 64 {
		writeErr(w, 400, "browser: 1 to 60 characters; version: at most 30; instance: at most 64")
		return
	}
	if in.Instance != "" && !in.Replace {
		pins, err := s.livePins(r.Context())
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if pin, ok := pins[in.Browser]; ok && pin.Instance != in.Instance {
			writeErr(w, http.StatusConflict, fmt.Sprintf("another copy of %s (%s) is connected; to use this copy instead, press Connect in its popup, or Use this browser in Browser Foreman", in.Browser, shortInstance(pin.Instance)))
			return
		}
	}
	now := s.Now()
	key := "pairreq:" + clientKey(r)
	if ok, until := s.Limit.Allowed(key, now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(until.Sub(now).Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many requests to connect; try again later")
		return
	}
	poll, err := auth.NewSecret("pr_")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	q := &pairRequest{ID: "rq_" + newID()[3:], Browser: in.Browser, Version: in.Version, Instance: in.Instance, Where: s.locate(r), Match: newMatch(),
		pollHash: auth.HashSecret(poll), Created: now.UTC(), Expires: now.Add(requestTTL), State: "pending"}
	s.ext.mu.Lock()
	s.ext.init()
	s.ext.prune(now)
	waiting := 0
	for _, x := range s.ext.requests {
		if x.State == "pending" {
			waiting++
		}
	}
	if waiting >= maxRequests {
		s.ext.mu.Unlock()
		s.Limit.Fail(key, now) // asking again and again counts against the caller
		writeErr(w, http.StatusTooManyRequests, "too many requests are waiting for approval; approve or deny them first")
		return
	}
	s.ext.requests[q.ID] = q
	s.ext.mu.Unlock()
	_, _ = s.Bus.Publish("extension.requested", map[string]any{"browser": q.Browser, "request": q.ID})
	if s.Console != nil {
		fmt.Fprintf(s.Console, "%s's extension asks to connect, showing %s. Approve it in Browser Foreman, or run `browser-foreman pair --approve` and type %s. It waits until %s.\n",
			q.Browser, q.Match, q.Match, q.Expires.Local().Format("15:04"))
	}
	writeJSON(w, 201, map[string]any{"id": q.ID, "poll": poll, "match": q.Match, "expires": q.Expires})
}

// pollPairing is the extension asking whether its request was answered. Approved, it gets its token, once.
func (s *Server) pollPairing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Poll string `json:"poll"`
	}
	if !decode(w, r, &in) {
		return
	}
	now := s.Now()
	s.ext.mu.Lock()
	s.ext.init()
	s.ext.prune(now)
	q := s.ext.requests[r.PathValue("id")]
	if q == nil || subtle.ConstantTimeCompare([]byte(auth.HashSecret(in.Poll)), []byte(q.pollHash)) != 1 {
		s.ext.mu.Unlock()
		writeErr(w, 404, "this connection request has expired or was answered; ask again with Connect")
		return
	}
	state := q.State
	if state == "answering" {
		state = "pending" // the owner's answer is being made: ask again in a moment, the token is not lost
	}
	out := map[string]any{"state": state}
	if state != "pending" {
		if q.State == "approved" {
			out["token"], out["browser"] = q.token, q.Browser
		}
		delete(s.ext.requests, q.ID) // answered: the token is handed over once
	}
	s.ext.mu.Unlock()
	writeJSON(w, 200, out)
}

// listPairingRequests shows the owner what is waiting, with each match code.
func (s *Server) listPairingRequests(w http.ResponseWriter, r *http.Request) {
	pins, err := s.livePins(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.ext.mu.Lock()
	s.ext.init()
	s.ext.prune(s.Now())
	out := []map[string]any{}
	for _, q := range s.ext.requests {
		if q.State == "pending" {
			x := map[string]any{"id": q.ID, "browser": q.Browser, "version": q.Version, "match": q.Match, "created": q.Created, "expires": q.Expires,
				"instance": shortInstance(q.Instance)}
			if q.Where != nil {
				x["where"] = whereView(q.Where)
			}
			// Another copy of this browser is the connected one: approving this one moves the browser here.
			if pin, ok := pins[q.Browser]; ok && pin.Instance != q.Instance {
				x["replaces"] = shortInstance(pin.Instance)
			}
			out = append(out, x)
		}
	}
	s.ext.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i]["created"].(time.Time).Before(out[j]["created"].(time.Time)) })
	writeJSON(w, 200, map[string]any{"requests": out})
}

// answerPairing approves or denies a request (owner). Approving makes the extension's token.
func (s *Server) answerPairing(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := s.Now()
		s.ext.mu.Lock()
		s.ext.init()
		s.ext.prune(now)
		q := s.ext.requests[r.PathValue("id")]
		if q == nil || q.State != "pending" {
			s.ext.mu.Unlock()
			writeErr(w, 404, "that connection request has expired (they wait five minutes) or was already answered; the extension asks again within a minute, or press Connect in its popup")
			return
		}
		q.State = "answering" // no second answer while the token is made
		s.ext.mu.Unlock()
		state, token := "denied", ""
		if approve {
			secret, id, err := s.mintExtensionToken(r.Context(), principal(r), q.Browser)
			if err != nil {
				s.ext.mu.Lock()
				q.State = "pending"
				s.ext.mu.Unlock()
				writeErr(w, 500, err.Error())
				return
			}
			revoked, err := s.pinExtension(r.Context(), q.Browser, q.Instance, q.Version, id)
			if err != nil {
				_ = s.Data.RevokeToken(r.Context(), id) // half-done: no key without its pin
				s.ext.mu.Lock()
				q.State = "pending"
				s.ext.mu.Unlock()
				writeErr(w, 500, err.Error())
				return
			}
			state, token = "approved", secret
			_, _ = s.Bus.Publish("extension.paired", map[string]any{"browser": q.Browser, "token": id, "instance": shortInstance(q.Instance), "replaced": revoked})
		}
		s.ext.mu.Lock()
		q.State, q.token = state, token
		q.Expires = now.Add(requestTTL) // time to collect the answer
		s.ext.mu.Unlock()
		writeJSON(w, 200, map[string]any{"state": state, "browser": q.Browser})
	}
}

// mintExtensionToken makes a paired extension's token for p (local mode: the "local" account).
func (s *Server) mintExtensionToken(ctx context.Context, p Principal, browser string) (secret, id string, err error) {
	user := p.User
	if p.Via == "local" {
		if err := s.localAccount(ctx); err != nil {
			return "", "", err
		}
	}
	if secret, err = auth.NewSecret("brofo_"); err != nil {
		return "", "", err
	}
	t := store.Token{ID: "tk_" + newID()[3:], User: user, Name: extPrefix + browser, Hash: auth.HashSecret(secret), Created: s.Now().UTC()}
	return secret, t.ID, s.Data.CreateToken(ctx, t)
}

// extensionTokenMay reports whether an extension's token may use a route besides its own extension routes: reading
// the workspace list and one workspace (to write a workspace back into its browser). Nothing else: an extension's
// token cannot change settings, manage tokens or edit a workspace through the general API.
func extensionTokenMay(r *http.Request) bool {
	return r.Pattern == "GET /api/v1/workspaces" || r.Pattern == "GET /api/v1/workspaces/{id}"
}

// extensionWritable reports whether an extension may read its browser's bookmarks into workspace id: its own
// "<browser>-live", "<browser>-before-write" and "<browser>-target" (the write-back plan it was asked to write, which
// it reads back after writing), or a workspace already read from this browser by its extension (writing one back
// re-reads it there). Any other workspace stays out of its reach.
func (s *Server) extensionWritable(ctx context.Context, browser, id string) (bool, error) {
	if id == slug(browser)+"-live" || id == slug(browser)+"-before-write" || id == slug(browser)+"-target" {
		return true, nil
	}
	hist, err := s.Data.History(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	mark := " from " + browser + " (extension)"
	for _, e := range hist {
		if e.Op == "import" && strings.HasSuffix(e.Summary, mark) {
			return true, nil
		}
	}
	return false, nil
}

// ---- guided install ----

// extensionBuild is which build a browser takes and the page where extensions are added to it ("" for Safari,
// whose extensions are turned on in its settings).
func extensionBuild(b browsers.Browser) (target, page string) {
	switch b.Engine {
	case "chromium":
		pages := map[string]string{"Microsoft Edge": "edge://extensions", "Brave Browser": "brave://extensions", "Opera": "opera://extensions", "Vivaldi": "vivaldi://extensions"}
		if p, ok := pages[b.Name]; ok {
			return "chromium", p
		}
		return "chromium", "chrome://extensions"
	case "gecko":
		return "firefox", "about:debugging#/runtime/this-firefox"
	case "webkit":
		return "safari", ""
	}
	return "", ""
}

// extensionDir finds a built extension folder: web/dist/extension/<target> in the checkout the binary runs from
// (build/<os>/browser-foreman is two levels below it) or the current folder. "" when it has not been built.
func (s *Server) extensionDir(target string) string {
	if s.ExtensionRoot != "" {
		return dirIfBuilt(filepath.Join(s.ExtensionRoot, target))
	}
	var roots []string
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			roots = append(roots, filepath.Join(filepath.Dir(exe), "..", ".."))
		}
	}
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	for _, root := range roots {
		if d := dirIfBuilt(filepath.Join(root, "web", "dist", "extension", target)); d != "" {
			return d
		}
	}
	return ""
}

func dirIfBuilt(d string) string {
	if _, err := os.Stat(filepath.Join(d, "manifest.json")); err != nil {
		return ""
	}
	if abs, err := filepath.Abs(d); err == nil {
		return abs
	}
	return d
}

// extensionGuide lists, for each installed browser, which build it takes, whether that build is here, and where its
// extensions page is: what the Browser extensions dialog shows.
func (s *Server) extensionGuide(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	here := s.locate(r)
	var hereBrowser string
	if s.Browsers != nil {
		loc := s.locator()
		for _, b := range s.Browsers.Installed() {
			target, page := extensionBuild(b)
			if target == "" {
				continue
			}
			x := map[string]any{"browser": b.Name, "target": target, "page": page, "built": s.extensionDir(target) != ""}
			if here != nil && b.Path != "" && browsers.ExeName(here.App) == browsers.ExeName(b.Path) {
				hereBrowser = b.Name
			}
			if loc != nil {
				running := []map[string]any{}
				for _, in := range loc.Running(b) {
					running = append(running, whereView(&in))
				}
				x["running"] = running
			}
			out = append(out, x)
		}
	}
	s.ext.mu.Lock()
	seen := map[string]seenExt{}
	for k, v := range s.ext.seen {
		seen[k] = v
	}
	s.ext.mu.Unlock()
	res := map[string]any{"browsers": out, "seen": seen}
	if here != nil {
		// The browser this page runs in: Set it up for that browser opens here.
		v := whereView(here)
		v["browser"] = hereBrowser
		res["here"] = v
	}
	writeJSON(w, 200, res)
}

// openExtensionSetup opens a browser's extensions page and shows the build folder in Finder (owner). The page is the
// fixed one for that browser; nothing in the request becomes an address or a path.
func (s *Server) openExtensionSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Browser string `json:"browser"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.Browsers == nil || s.Browsers.Platform == nil {
		writeErr(w, 501, browsers.ErrUnsupported.Error())
		return
	}
	var b *browsers.Browser
	for _, x := range s.Browsers.Installed() {
		if x.Name == in.Browser {
			b = &x
			break
		}
	}
	if b == nil {
		writeErr(w, 404, "that browser is not installed on this computer, so it cannot be set up here")
		return
	}
	target, page := extensionBuild(*b)
	dir := s.extensionDir(target)
	if dir == "" {
		writeErr(w, 409, "the extension is not built yet: run `make extension` in the browser-foreman folder")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	// Open the extensions page as a tab in the copy of the browser this page runs in (its frontmost window), not in
	// whichever copy the system would pick: two copies (yours and an automated one) both answer to the app's name.
	var copyIn *browsers.Instance
	if page != "" && b.Path != "" {
		if loc := s.locator(); loc != nil {
			if copyIn = setupInstance(*b, s.locate(r), loc.Running(*b)); copyIn != nil {
				if err := loc.OpenIn(ctx, *copyIn, page); err != nil {
					copyIn = nil // fall back to the system's pick
				}
			}
		}
		if copyIn == nil {
			if err := s.Browsers.Platform.Launch(ctx, []string{"open", "-a", b.Path, page}); err != nil {
				writeErr(w, 502, "could not open "+b.Name+": "+err.Error())
				return
			}
		}
	}
	if err := s.Browsers.Platform.Launch(ctx, []string{"open", "-R", filepath.Join(dir, "manifest.json")}); err != nil {
		writeErr(w, 502, "could not show the folder: "+err.Error())
		return
	}
	res := map[string]any{"opened": page, "folder": dir}
	if copyIn != nil {
		res["in"] = whereView(copyIn)
	}
	writeJSON(w, 200, res)
}

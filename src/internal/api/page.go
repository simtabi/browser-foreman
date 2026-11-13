package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/webui"
)

// pageCSP lets the page run its own scripts and nothing else: files from this server, and inline scripts carrying
// this response's nonce. 'unsafe-eval' is there for Alpine, which compiles its x-* attributes with new Function; it
// can only evaluate what the page's own markup holds. Images may come from any https host (site icons).
func pageCSP(nonce string) string {
	return "default-src 'self'; script-src 'self' 'nonce-" + nonce + "' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: https:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
}

// ui is the web interface's files: Server.UI when set (tests), else what the binary embeds.
func (s *Server) ui() fs.FS {
	if s.UI != nil {
		return s.UI
	}
	return webui.Assets()
}

func newNonce() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func plainPage(w http.ResponseWriter, code int, nonce, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", pageCSP(nonce))
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer"><title>` + html.EscapeString(title) + ` · Browser Foreman</title>
<link rel="stylesheet" href="assets/css/tabler.min.css"></head>
<body class="d-flex flex-column"><div class="page page-center"><div class="container container-tight py-4">` + body + `</div></div></body></html>`))
}

// page serves the web interface for one workspace (?ws=, or the first). In local mode the page carries the configured
// token, which only this computer can read (the Host and Origin checks keep other sites from reading the page). In
// accounts mode it needs a session; without one, it is the sign-in page.
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if !webui.Built(s.ui()) {
		s.home(w, r)
		return
	}
	nonce := newNonce()
	auth := map[string]string{}
	if s.Cfg.Server.Mode == config.ModeLocal {
		if s.Cfg.APIToken == "" {
			plainPage(w, 503, nonce, "Set up", `<h1 class="h2">Almost there</h1><p>No API token is set, so the interface cannot save. Run
<code>browser-foreman config init</code>, then restart the server.</p>`)
			return
		}
		auth["token"] = s.Cfg.APIToken
	} else {
		p, ok := s.who(r)
		if !ok || p.Via != "session" {
			s.signInPage(w, nonce)
			return
		}
		auth["csrf"] = p.csrf
	}
	all, err := s.Data.Workspaces(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(all) == 0 {
		plainPage(w, 200, nonce, "No bookmarks yet", `<h1 class="h2">No bookmarks yet</h1>
<p>Import a browser's bookmarks file to start, then reload this page:</p>
<pre class="p-2 bg-light rounded">browser-foreman import ~/Downloads/bookmarks.html</pre>
<p class="text-secondary">Chrome, Edge, Brave, Vivaldi, Opera and Arc: their <code>Bookmarks</code> file or an export. Firefox and Safari: an exported HTML file.</p>`)
		return
	}
	live := all[:0:0] // the workspace Trash is never opened as a page
	for _, a := range all {
		if a.Deleted.IsZero() {
			live = append(live, a)
		}
	}
	all = live
	if len(all) == 0 {
		plainPage(w, 200, nonce, "Every workspace is in the Trash", `<h1 class="h2">Every workspace is in the workspace Trash</h1>
<p>Restore one with <code>POST /api/v1/workspaces/{id}/restore</code> (list them with <code>GET /api/v1/workspaces?trash=1</code>), then reload this page.</p>`)
		return
	}
	x := all[0] // the first one not archived, unless every one is
	for i := len(all) - 1; i >= 0; i-- {
		if !all[i].Archived {
			x = all[i]
		}
	}
	if id := r.URL.Query().Get("ws"); id != "" {
		found := false
		for _, a := range all {
			if a.ID == id {
				x, found = a, true
			}
		}
		if !found {
			plainPage(w, 404, nonce, "Not found", `<h1 class="h2">No workspace with that id</h1><p><a href="./">Open the first one</a></p>`)
			return
		}
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	h, err := s.Data.History(r.Context(), x.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	links, err := s.Data.Links(r.Context(), webURLs(t, nil))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	built := x.Created
	if n := len(h); n > 0 {
		built = h[n-1].At
	}
	boot, err := json.Marshal(map[string]any{ // json.Marshal escapes <, >, &, U+2028 and U+2029: safe inside <script>
		"workspace":  map[string]any{"id": x.ID, "name": x.Name, "head": x.Head, "notes": x.Notes, "locked": x.Locked, "archived": x.Archived},
		"workspaces": all,
		"tree":       t, "built": built.UTC().Format(time.RFC3339), "can": canUndo(h), "auth": auth, "links": links,
		"media": s.mediaFor(t),
	})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	body, err := webui.Page(s.ui(), boot, nonce)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", pageCSP(nonce))
	_, _ = w.Write(body)
}

func (s *Server) signInPage(w http.ResponseWriter, nonce string) {
	plainPage(w, 200, nonce, "Sign in", `<form class="card card-md" id="f" autocomplete="on"><div class="card-body">
<h1 class="h2 text-center mb-4">Sign in to Browser Foreman</h1>
<div class="mb-3"><label class="form-label" for="u">Name</label><input class="form-control" id="u" name="username" autocomplete="username" required autofocus></div>
<div class="mb-2"><label class="form-label" for="p">Password</label><input class="form-control" id="p" name="password" type="password" autocomplete="current-password" required></div>
<div class="text-danger small mb-2" id="e" role="alert"></div>
<button class="btn btn-primary w-100" type="submit">Sign in</button></div></form>
<script nonce="`+nonce+`">
document.getElementById("f").addEventListener("submit", async e => {
  e.preventDefault();
  const out = document.getElementById("e"); out.textContent = "";
  const r = await fetch("api/v1/auth/signin", {method: "POST", headers: {"Content-Type": "application/json"},
    body: JSON.stringify({username: document.getElementById("u").value, password: document.getElementById("p").value})});
  if (r.ok) return location.reload();
  const j = await r.json().catch(() => ({}));
  out.textContent = j.error || "Could not sign in (" + r.status + ")";
});
</script>`)
}

// assets serves the page's stylesheets, fonts and scripts. Only files: a directory path ends in "/", which fs.Stat
// refuses, so no listing is ever served.
func (s *Server) assets() http.Handler {
	files := http.FileServerFS(s.ui())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(s.ui(), strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}

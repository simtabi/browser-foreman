package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// iconCache holds converted browser icons for the life of the server.
type iconCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func (s *Server) listBrowsers(w http.ResponseWriter, r *http.Request) {
	p, err := store.LoadPrefs(r.Context(), s.Data)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"installed": s.Browsers.Installed(), "default": s.Browsers.Default(),
		"open_with": p.OpenWith, "tracked": p.Tracked})
}

// browserIcon answers an installed browser's own app icon as a 64-pixel PNG (?name=Google Chrome).
func (s *Server) browserIcon(w http.ResponseWriter, r *http.Request) {
	b, ok := s.Browsers.Find(r.URL.Query().Get("name"))
	if !ok {
		writeErr(w, 404, "no installed browser by that name")
		return
	}
	s.icons.mu.Lock()
	png, cached := s.icons.m[b.Bundle]
	s.icons.mu.Unlock()
	if !cached {
		var err error
		if png, err = s.Browsers.Platform.Icon(b, 64); err != nil {
			writeErr(w, 404, "no icon: "+err.Error())
			return
		}
		s.icons.mu.Lock()
		if s.icons.m == nil {
			s.icons.m = map[string][]byte{}
		}
		s.icons.m[b.Bundle] = png
		s.icons.mu.Unlock()
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(png)
}

// openLinks hands links to browsers on this computer: the ones named, else the chosen ones (preferences), else the
// system default. Owner only: it starts programs on the machine the server runs on. The event names no address, so
// private links do not reach webhooks.
func (s *Server) openLinks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URLs     []string `json:"urls"`
		How      string   `json:"how"`
		Browsers []string `json:"browsers"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Browsers) > 12 {
		writeErr(w, 400, "at most 12 browsers")
		return
	}
	if len(in.Browsers) == 0 {
		p, err := store.LoadPrefs(r.Context(), s.Data)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		in.Browsers = p.OpenWith
	}
	res, err := s.Browsers.Open(r.Context(), in.URLs, in.How, in.Browsers)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ok := 0
	for _, o := range res {
		if o.OK {
			ok++
		}
	}
	_, _ = s.Bus.Publish("links.opened", map[string]any{"count": len(in.URLs), "how": in.How, "browsers": in.Browsers, "ok": ok})
	code := 200
	if ok == 0 {
		code = 502
	}
	writeJSON(w, code, map[string]any{"opened": res})
}

func (s *Server) getPrefs(w http.ResponseWriter, r *http.Request) {
	p, err := store.LoadPrefs(r.Context(), s.Data)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

// putPrefs replaces the preferences; fields left out keep their current values.
func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10)) // read before the lock: a slow client holds nothing; 50 smart folders fit
	if err != nil {
		writeErr(w, 400, "bad JSON body: "+err.Error())
		return
	}
	p, err := store.UpdatePrefs(r.Context(), s.Data, func(p *store.Prefs) error {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.DisallowUnknownFields()
		if err := dec.Decode(p); err != nil {
			return fmt.Errorf("bad JSON body: %w", err)
		}
		return nil
	})
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("preferences.updated", map[string]any{"open_with": p.OpenWith, "autosave": p.Autosave})
	writeJSON(w, 200, p)
}

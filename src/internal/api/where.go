package api

import (
	"net/http"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// Two copies of one browser can run at once (your Chrome and another, say one an assistant drives with a profile of
// its own), and one copy can have many windows. Where the platform can tell (macOS), the server finds which running
// copy a page or an extension calls from, so Set it up opens in that copy's window, and the dialog can say which
// copy is which.

func (s *Server) locator() browsers.Locator {
	if s.Browsers == nil {
		return nil
	}
	loc, _ := s.Browsers.Platform.(browsers.Locator)
	return loc
}

// locate is the running browser behind r's connection; nil when it cannot be told.
func (s *Server) locate(r *http.Request) *browsers.Instance {
	loc := s.locator()
	if loc == nil {
		return nil
	}
	in, err := loc.PageInstance(r.RemoteAddr)
	if err != nil {
		return nil
	}
	return &in
}

// whereView is how a running copy shows in the API: its app folder, its own profile folder if it has one, and
// whether automation started it.
func whereView(in *browsers.Instance) map[string]any {
	if in == nil {
		return nil
	}
	v := map[string]any{"app": in.App, "automated": in.Automated}
	if in.Profile != "" {
		v["profile"] = in.Profile
	}
	return v
}

// setupInstance is the running copy of b that Set it up opens in: the one the page runs in when that is b, else the
// one copy of b a person started, else the only copy running. nil leaves it to the system (open -a).
func setupInstance(b browsers.Browser, page *browsers.Instance, running []browsers.Instance) *browsers.Instance {
	name := browsers.ExeName(b.Path)
	if page != nil && browsers.ExeName(page.App) == name {
		return page
	}
	var people []browsers.Instance
	for _, in := range running {
		if !in.Automated {
			people = append(people, in)
		}
	}
	if len(people) == 1 {
		return &people[0]
	}
	if len(running) == 1 {
		return &running[0]
	}
	return nil
}

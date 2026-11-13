// Package browsers knows the web browsers on this computer: which are installed, which one the system opens links
// in, their icons, and how to ask one to open links as tabs, in a new window or in a private window. What to run is
// worked out by pure functions (Command); running it goes through a Platform, so tests never start a browser.
package browsers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Browser is one browser this tool knows, and what the system lets it do with it.
type Browser struct {
	Name   string `json:"name"`
	Bundle string `json:"bundle"`
	Engine string `json:"engine"` // chromium, webkit or gecko
	// Scriptable: its windows and tabs can be read (and closed) through automation.
	Scriptable bool `json:"scriptable"`
	// SessionFile: its tabs are read from the file it keeps to restore its session (Firefox everywhere; every
	// browser on Linux and Windows). Private windows are never in it.
	SessionFile bool `json:"session_file,omitempty"`
	// PrivateKnown: a tab read can tell private windows apart.
	PrivateKnown bool `json:"private_known"`
	// PrivateFlag is the launch option for a new private window; "" when it has none.
	PrivateFlag string `json:"private_flag,omitempty"`
	Path        string `json:"path,omitempty"` // where it is installed; "" in Known
}

// SystemDefault names the browser the system opens links in, resolved when a link opens.
const SystemDefault = "System default"

// Known lists every browser this tool can work with, in the order lists show them.
var Known = []Browser{
	{Name: "Google Chrome", Bundle: "com.google.Chrome", Engine: "chromium", Scriptable: true, PrivateKnown: true, PrivateFlag: "--incognito"},
	{Name: "Google Chrome Canary", Bundle: "com.google.Chrome.canary", Engine: "chromium", Scriptable: true, PrivateKnown: true, PrivateFlag: "--incognito"},
	{Name: "Chromium", Bundle: "org.chromium.Chromium", Engine: "chromium", Scriptable: true, PrivateKnown: true, PrivateFlag: "--incognito"},
	{Name: "Brave Browser", Bundle: "com.brave.Browser", Engine: "chromium", Scriptable: true, PrivateKnown: true, PrivateFlag: "--incognito"},
	{Name: "Microsoft Edge", Bundle: "com.microsoft.edgemac", Engine: "chromium", Scriptable: true, PrivateKnown: true, PrivateFlag: "--inprivate"},
	{Name: "Vivaldi", Bundle: "com.vivaldi.Vivaldi", Engine: "chromium", Scriptable: true, PrivateKnown: true, PrivateFlag: "--incognito"},
	{Name: "Opera", Bundle: "com.operasoftware.Opera", Engine: "chromium", Scriptable: true, PrivateKnown: true, PrivateFlag: "--private"},
	{Name: "Arc", Bundle: "company.thebrowser.Browser", Engine: "chromium", Scriptable: true},
	{Name: "Safari", Bundle: "com.apple.Safari", Engine: "webkit", Scriptable: true},
	{Name: "Firefox", Bundle: "org.mozilla.firefox", Engine: "gecko", PrivateFlag: "--private-window", SessionFile: true},
	{Name: "Zen", Bundle: "app.zen-browser.zen", Engine: "gecko", PrivateFlag: "--private-window", SessionFile: true},
	{Name: "Orion", Bundle: "com.kagi.kagimacOS", Engine: "webkit"},
}

// Platform is what the operating system provides. The macOS one is in platform_darwin.go; other systems get one
// that finds nothing and opens nothing, until they are supported.
type Platform interface {
	// Installed returns the known browsers that are installed, with their paths.
	Installed() []Browser
	// DefaultBundle is the bundle id of the browser the system opens https links in, "" when unknown.
	DefaultBundle() string
	// Icon returns a browser's app icon as a PNG, size pixels square.
	Icon(b Browser, size int) ([]byte, error)
	// Launch runs a command that hands links to a browser and returns once it has been handed over (or refused).
	Launch(ctx context.Context, argv []string) error
}

// Commander is a Platform that opens links its own way (Linux, Windows); macOS's open(1) is Command below.
type Commander interface {
	// Command is what to run to open urls in b.
	Command(b Browser, how string, urls []string) ([]string, error)
	// DefaultCommands are what to run to open urls in the system's default browser, one command per link.
	DefaultCommands(urls []string) [][]string
}

// ErrUnsupported: this operating system is not supported yet for this.
var ErrUnsupported = errors.New("opening links in a browser is supported on macOS so far")

// How a set of links opens.
const (
	AsTabs    = "tabs"
	NewWindow = "window"
	Private   = "incognito"
)

// MaxLinks is the most links one request may open.
const MaxLinks = 200

// CheckLinks accepts only web addresses: other schemes could start any application on this computer.
func CheckLinks(urls []string) error {
	if len(urls) == 0 {
		return errors.New("give at least one link")
	}
	if len(urls) > MaxLinks {
		return fmt.Errorf("at most %d links at once", MaxLinks)
	}
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%q: only http and https links can be opened", raw)
		}
		if strings.HasPrefix(raw, "-") {
			return fmt.Errorf("%q is not a link", raw)
		}
	}
	return nil
}

// Command is what to run to open urls in b: as tabs in a running window, in a new window, or in a new private window
// using the browser's own launch option. A browser with no private window option is refused rather than opening the
// links in a normal window by mistake.
func Command(b Browser, how string, urls []string) ([]string, error) {
	switch how {
	case AsTabs, "":
		return append([]string{"open", "-a", b.Path}, urls...), nil
	case NewWindow:
		if b.Engine == "chromium" || b.Engine == "gecko" {
			return append([]string{"open", "-na", b.Path, "--args", "--new-window"}, urls...), nil
		}
		return append([]string{"open", "-a", b.Path}, urls...), nil
	case Private:
		if b.PrivateFlag == "" {
			return nil, fmt.Errorf("%s cannot be asked for a private window; choose another browser for that", b.Name)
		}
		return append([]string{"open", "-na", b.Path, "--args", b.PrivateFlag}, urls...), nil
	}
	return nil, fmt.Errorf("how must be tabs, window or incognito")
}

// Service answers questions about browsers and opens links, caching what is installed.
type Service struct {
	Platform Platform
	mu       sync.Mutex
	cache    []Browser
}

// Installed lists the installed browsers, in Known order.
func (s *Service) Installed() []Browser {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = s.Platform.Installed()
		if s.cache == nil {
			s.cache = []Browser{}
		}
	}
	return s.cache
}

// Refresh forgets the cached list (after installing or removing a browser).
func (s *Service) Refresh() { s.mu.Lock(); s.cache = nil; s.mu.Unlock() }

// Find returns an installed browser by name.
func (s *Service) Find(name string) (Browser, bool) {
	for _, b := range s.Installed() {
		if b.Name == name {
			return b, true
		}
	}
	return Browser{}, false
}

// Default is the installed browser the system opens links in, or "" when it is unknown or one this tool does not know.
func (s *Service) Default() string {
	bid := strings.ToLower(s.Platform.DefaultBundle())
	for _, b := range s.Installed() {
		if strings.ToLower(b.Bundle) == bid {
			return b.Name
		}
	}
	return ""
}

// Opened is what happened in one browser.
type Opened struct {
	Browser string `json:"browser"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

// Open opens urls in each named browser (SystemDefault resolves to the system's browser), all at once. Each browser
// answers on its own: one that cannot is reported and the others still open.
func (s *Service) Open(ctx context.Context, urls []string, how string, names []string) ([]Opened, error) {
	if err := CheckLinks(urls); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		names = []string{SystemDefault}
	}
	out := make([]Opened, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = s.openIn(ctx, urls, how, name)
		}()
	}
	wg.Wait()
	return out, nil
}

func (s *Service) openIn(ctx context.Context, urls []string, how, name string) Opened {
	if name == SystemDefault {
		if d := s.Default(); d != "" {
			name = d
		} else {
			if how == Private {
				return Opened{Browser: name, Error: "the system default browser cannot be asked for a private window; choose a browser for that"}
			}
			cmds := [][]string{append([]string{"open"}, urls...)}
			if c, ok := s.Platform.(Commander); ok {
				cmds = c.DefaultCommands(urls)
			}
			for _, argv := range cmds {
				if err := s.Platform.Launch(ctx, argv); err != nil {
					return Opened{Browser: name, Error: err.Error()}
				}
			}
			return Opened{Browser: name, OK: true}
		}
	}
	b, ok := s.Find(name)
	if !ok {
		return Opened{Browser: name, Error: name + " is not installed"}
	}
	command := Command
	if c, ok := s.Platform.(Commander); ok {
		command = c.Command
	}
	argv, err := command(b, how, urls)
	if err == nil {
		err = s.Platform.Launch(ctx, argv)
	}
	if err != nil {
		return Opened{Browser: name, Error: err.Error()}
	}
	return Opened{Browser: name, OK: true}
}

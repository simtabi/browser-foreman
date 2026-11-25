package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// fakeLocator is two copies of Chrome running: yours from the Desktop, and an automated one with a profile of its
// own. The page runs in page; OpenIn is recorded, never run.
type fakeLocator struct {
	fakeBrowsers
	page   *browsers.Instance
	opened []string
}

var (
	yours     = browsers.Instance{PID: 7, App: "/Users/me/Desktop/Google Chrome.app", Exe: "/Users/me/Desktop/Google Chrome.app/Contents/MacOS/Google Chrome"}
	automated = browsers.Instance{PID: 8, App: "/Applications/Google Chrome.app", Exe: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", Profile: "/tmp/agent", Automated: true}
)

func (f *fakeLocator) PageInstance(string) (browsers.Instance, error) {
	if f.page == nil {
		return browsers.Instance{}, fmt.Errorf("none")
	}
	return *f.page, nil
}
func (f *fakeLocator) Running(b browsers.Browser) []browsers.Instance {
	if b.Name == "Google Chrome" {
		return []browsers.Instance{automated, yours}
	}
	return nil
}
func (f *fakeLocator) OpenIn(_ context.Context, in browsers.Instance, url string) error {
	f.opened = append(f.opened, in.App+" "+url)
	return nil
}

func TestSetupOpensInThePagesOwnCopy(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	loc := &fakeLocator{}
	s.Browsers.Platform = loc
	s.ExtensionRoot = t.TempDir()
	dir := filepath.Join(s.ExtensionRoot, "chromium")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The page runs in the automated copy: Set it up opens there, not in yours.
	loc.page = &automated
	if w := send(h, "POST", "/api/v1/extension/setup", `{"browser":"Google Chrome"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"automated":true`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(loc.opened) != 1 || loc.opened[0] != automated.App+" chrome://extensions" {
		t.Fatalf("opened %q", loc.opened)
	}
	// The page runs in yours: there. And the guide says which copy the page is in, and lists both.
	loc.page = &yours
	send(h, "POST", "/api/v1/extension/setup", `{"browser":"Google Chrome"}`)
	if loc.opened[1] != yours.App+" chrome://extensions" {
		t.Fatalf("opened %q", loc.opened)
	}
	guide := send(h, "GET", "/api/v1/extension/guide", "").Body.String()
	if !strings.Contains(guide, `"here":{"app":"/Users/me/Desktop/Google Chrome.app","automated":false,"browser":"Google Chrome"}`) || strings.Count(guide, `"app":`) != 3 {
		t.Fatalf("guide %s", guide)
	}
	// Opened from Safari: the one copy a person started.
	loc.page = &browsers.Instance{App: "/Applications/Safari.app"}
	send(h, "POST", "/api/v1/extension/setup", `{"browser":"Google Chrome"}`)
	if loc.opened[2] != yours.App+" chrome://extensions" {
		t.Fatalf("opened %q", loc.opened)
	}
	for _, argv := range loc.ran {
		if argv[1] == "-a" {
			t.Fatalf("fell back to the system's pick: %q", argv)
		}
	}
}

func TestSetupInstanceChoice(t *testing.T) {
	chrome := browsers.Browser{Name: "Google Chrome", Path: "/Applications/Google Chrome.app"}
	if got := setupInstance(chrome, nil, nil); got != nil {
		t.Fatalf("nothing running: %+v", got)
	}
	if got := setupInstance(chrome, nil, []browsers.Instance{automated}); got == nil || got.PID != 8 {
		t.Fatalf("the only copy: %+v", got)
	}
	if got := setupInstance(chrome, nil, []browsers.Instance{yours, {PID: 9, App: "/Applications/Google Chrome.app"}}); got != nil {
		t.Fatalf("two of yours, no page: the system decides: %+v", got)
	}
}

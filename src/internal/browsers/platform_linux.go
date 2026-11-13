package browsers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// linuxBrowsers: each known browser's executable names and its desktop file, which names it as the default browser
// (xdg-settings) and as an icon in the icon theme.
var linuxBrowsers = map[string]struct {
	exes    []string
	desktop string
}{
	"Google Chrome":  {[]string{"google-chrome", "google-chrome-stable"}, "google-chrome"},
	"Chromium":       {[]string{"chromium", "chromium-browser"}, "chromium"},
	"Brave Browser":  {[]string{"brave-browser", "brave"}, "brave-browser"},
	"Microsoft Edge": {[]string{"microsoft-edge", "microsoft-edge-stable"}, "microsoft-edge"},
	"Vivaldi":        {[]string{"vivaldi", "vivaldi-stable"}, "vivaldi-stable"},
	"Opera":          {[]string{"opera"}, "opera"},
	"Firefox":        {[]string{"firefox"}, "firefox"},
	"Zen":            {[]string{"zen-browser", "zen"}, "zen"},
}

// NewPlatform finds browsers on PATH and opens links with them, or with xdg-open for the default browser.
func NewPlatform() Platform {
	return &execPlatform{find: linuxInstalled, dflt: linuxDefault, icon: linuxIcon, LaunchWait: 2 * time.Second,
		opener: func(u string) []string { return []string{"xdg-open", u} }}
}

func linuxInstalled() []Browser {
	var out []Browser
	for _, b := range Known {
		lb, ok := linuxBrowsers[b.Name]
		if !ok {
			continue
		}
		for _, exe := range lb.exes {
			if p, err := exec.LookPath(exe); err == nil {
				b.Path, b.Bundle = p, lb.desktop
				b.Scriptable, b.SessionFile = false, true // tabs are read from the session files, not by automation
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// linuxDefault is the default browser's desktop id without ".desktop" (xdg-settings).
func linuxDefault() string {
	out, err := exec.Command("xdg-settings", "get", "default-web-browser").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), ".desktop")
}

// linuxIcon reads the browser's icon from the icon theme, the largest size up to what was asked.
func linuxIcon(b Browser, size int) ([]byte, error) {
	for _, base := range []string{"/usr/share/icons/hicolor", filepath.Join(os.Getenv("HOME"), ".local/share/icons/hicolor")} {
		for _, s := range []int{256, 128, 64, 48, 32} {
			if s > size*2 && s != 32 {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(base, sizeDir(s), "apps", b.Bundle+".png")); err == nil {
				return b, nil
			}
		}
	}
	return nil, ErrUnsupported
}

func sizeDir(s int) string { n := strconv.Itoa(s); return n + "x" + n }

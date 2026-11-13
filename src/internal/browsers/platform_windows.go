package browsers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// winBrowsers: where each known browser installs (under these folders, tried in order) and the start of the ProgId
// Windows records for it as the https handler.
var winBrowsers = map[string]struct {
	paths  []string
	progID string
}{
	"Google Chrome":  {[]string{`%ProgramFiles%\Google\Chrome\Application\chrome.exe`, `%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe`, `%LOCALAPPDATA%\Google\Chrome\Application\chrome.exe`}, "ChromeHTML"},
	"Chromium":       {[]string{`%LOCALAPPDATA%\Chromium\Application\chrome.exe`}, "ChromiumHTM"},
	"Brave Browser":  {[]string{`%ProgramFiles%\BraveSoftware\Brave-Browser\Application\brave.exe`, `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\Application\brave.exe`}, "BraveHTML"},
	"Microsoft Edge": {[]string{`%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe`, `%ProgramFiles%\Microsoft\Edge\Application\msedge.exe`}, "MSEdgeHTM"},
	"Vivaldi":        {[]string{`%LOCALAPPDATA%\Vivaldi\Application\vivaldi.exe`, `%ProgramFiles%\Vivaldi\Application\vivaldi.exe`}, "VivaldiHTM"},
	"Opera":          {[]string{`%LOCALAPPDATA%\Programs\Opera\opera.exe`}, "Opera"},
	"Firefox":        {[]string{`%ProgramFiles%\Mozilla Firefox\firefox.exe`, `%ProgramFiles(x86)%\Mozilla Firefox\firefox.exe`}, "FirefoxURL"},
}

// NewPlatform finds browsers where they install and opens links with them, or through the URL handler for the
// default browser (rundll32 url.dll, which takes a link without a shell parsing it).
func NewPlatform() Platform {
	return &execPlatform{find: winInstalled, dflt: winDefault, icon: func(Browser, int) ([]byte, error) { return nil, ErrUnsupported },
		LaunchWait: 2 * time.Second,
		opener:     func(u string) []string { return []string{"rundll32", "url.dll,FileProtocolHandler", u} }}
}

func winInstalled() []Browser {
	var out []Browser
	for _, b := range Known {
		wb, ok := winBrowsers[b.Name]
		if !ok {
			continue
		}
		for _, p := range wb.paths {
			p = os.ExpandEnv(strings.NewReplacer("%ProgramFiles%", "${ProgramFiles}", "%ProgramFiles(x86)%", "${ProgramFiles(x86)}", "%LOCALAPPDATA%", "${LOCALAPPDATA}").Replace(p))
			if st, err := os.Stat(filepath.Clean(p)); err == nil && !st.IsDir() {
				b.Path, b.Bundle, b.Scriptable, b.SessionFile = p, wb.progID, false, true
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// winDefault is the ProgId of the https handler the user chose, cut to the browser's prefix (FirefoxURL-308046B0AF4A39CB
// is FirefoxURL).
func winDefault() string {
	out, err := exec.Command("reg", "query", `HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`, "/v", "ProgId").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 3 && f[0] == "ProgId" {
			id, _, _ := strings.Cut(f[2], "-")
			return id
		}
	}
	return ""
}

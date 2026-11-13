package tabs

import (
	"os"
	"path/filepath"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// On macOS automation reads Chromium browsers and Safari; session files are for the Firefox family, which it cannot.
func sessionDirs(b browsers.Browser) []string {
	h, _ := os.UserHomeDir()
	as := filepath.Join(h, "Library", "Application Support")
	switch b.Name {
	case "Firefox":
		return profilesIn(filepath.Join(as, "Firefox", "Profiles", "*"))
	case "Zen":
		return profilesIn(filepath.Join(as, "zen", "Profiles", "*"))
	}
	return nil
}

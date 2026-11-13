package importer

import (
	"os"
	"path/filepath"
)

func places() []place {
	h, _ := os.UserHomeDir()
	as := filepath.Join(h, "Library", "Application Support")
	return []place{
		chromium("Chrome", filepath.Join(as, "Google", "Chrome")),
		chromium("Chrome Beta", filepath.Join(as, "Google", "Chrome Beta")),
		chromium("Chromium", filepath.Join(as, "Chromium")),
		chromium("Edge", filepath.Join(as, "Microsoft Edge")),
		chromium("Brave", filepath.Join(as, "BraveSoftware", "Brave-Browser")),
		chromium("Vivaldi", filepath.Join(as, "Vivaldi")),
		{"Opera", filepath.Join(as, "com.operasoftware.Opera", "Bookmarks"), ""},
		chromium("Arc", filepath.Join(as, "Arc", "User Data")),
		{"Firefox", filepath.Join(as, "Firefox", "Profiles", "*", "places.sqlite"), ""},
		{"Safari", filepath.Join(h, "Library", "Safari", "Bookmarks.plist"), ""},
	}
}

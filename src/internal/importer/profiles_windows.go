package importer

import (
	"os"
	"path/filepath"
)

func places() []place {
	local, roaming := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
	return []place{
		chromium("Chrome", filepath.Join(local, "Google", "Chrome", "User Data")),
		chromium("Chrome Beta", filepath.Join(local, "Google", "Chrome Beta", "User Data")),
		chromium("Chromium", filepath.Join(local, "Chromium", "User Data")),
		chromium("Edge", filepath.Join(local, "Microsoft", "Edge", "User Data")),
		chromium("Brave", filepath.Join(local, "BraveSoftware", "Brave-Browser", "User Data")),
		chromium("Vivaldi", filepath.Join(local, "Vivaldi", "User Data")),
		{"Opera", filepath.Join(roaming, "Opera Software", "Opera Stable", "Bookmarks"), ""},
		{"Firefox", filepath.Join(roaming, "Mozilla", "Firefox", "Profiles", "*", "places.sqlite"), ""},
	}
}

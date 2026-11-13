package importer

import (
	"os"
	"path/filepath"
)

func places() []place {
	h, _ := os.UserHomeDir()
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(h, ".config")
	}
	return []place{
		chromium("Chrome", filepath.Join(cfg, "google-chrome")),
		chromium("Chrome Beta", filepath.Join(cfg, "google-chrome-beta")),
		chromium("Chromium", filepath.Join(cfg, "chromium")),
		chromium("Chromium (snap)", filepath.Join(h, "snap", "chromium", "common", "chromium")),
		chromium("Edge", filepath.Join(cfg, "microsoft-edge")),
		chromium("Brave", filepath.Join(cfg, "BraveSoftware", "Brave-Browser")),
		chromium("Vivaldi", filepath.Join(cfg, "vivaldi")),
		{"Opera", filepath.Join(cfg, "opera", "Bookmarks"), ""},
		{"Firefox", filepath.Join(h, ".mozilla", "firefox", "*", "places.sqlite"), ""},
		{"Firefox (snap)", filepath.Join(h, "snap", "firefox", "common", ".mozilla", "firefox", "*", "places.sqlite"), ""},
		{"Firefox (Flatpak)", filepath.Join(h, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox", "*", "places.sqlite"), ""},
	}
}

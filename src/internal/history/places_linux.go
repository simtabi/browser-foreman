package history

import (
	"os"
	"path/filepath"
)

// SettingsURL is empty: Linux has no Full Disk Access to grant.
const SettingsURL = ""

func places() []place {
	h, _ := os.UserHomeDir()
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(h, ".config")
	}
	c := func(browser, dir string) place {
		return place{browser, Chromium, filepath.Join(cfg, dir, "*", "History")}
	}
	return []place{
		c("Google Chrome", "google-chrome"), c("Chromium", "chromium"), c("Brave Browser", filepath.Join("BraveSoftware", "Brave-Browser")),
		c("Microsoft Edge", "microsoft-edge"), c("Vivaldi", "vivaldi"),
		{"Firefox", Firefox, filepath.Join(h, ".mozilla", "firefox", "*", "places.sqlite")},
		{"Firefox (snap)", Firefox, filepath.Join(h, "snap", "firefox", "common", ".mozilla", "firefox", "*", "places.sqlite")},
	}
}

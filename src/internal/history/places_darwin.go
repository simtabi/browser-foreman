package history

import (
	"os"
	"path/filepath"
)

// SettingsURL opens System Settings at Full Disk Access.
const SettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"

func places() []place {
	h, _ := os.UserHomeDir()
	as := filepath.Join(h, "Library", "Application Support")
	c := func(browser string, parts ...string) place {
		return place{browser, Chromium, filepath.Join(append(append([]string{as}, parts...), "*", "History")...)}
	}
	return []place{
		c("Google Chrome", "Google", "Chrome"), c("Brave Browser", "BraveSoftware", "Brave-Browser"),
		c("Microsoft Edge", "Microsoft Edge"), c("Arc", "Arc", "User Data"), c("Vivaldi", "Vivaldi"), c("Chromium", "Chromium"),
		{"Firefox", Firefox, filepath.Join(as, "Firefox", "Profiles", "*", "places.sqlite")},
		{"Safari", Safari, filepath.Join(h, "Library", "Safari", "History.db")},
	}
}

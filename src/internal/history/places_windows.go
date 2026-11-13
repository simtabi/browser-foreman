package history

import (
	"os"
	"path/filepath"
)

// SettingsURL is empty: Windows has no Full Disk Access to grant.
const SettingsURL = ""

func places() []place {
	local, roaming := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
	c := func(browser string, parts ...string) place {
		return place{browser, Chromium, filepath.Join(append(append([]string{local}, parts...), "User Data", "*", "History")...)}
	}
	return []place{
		c("Google Chrome", "Google", "Chrome"), c("Microsoft Edge", "Microsoft", "Edge"), c("Brave Browser", "BraveSoftware", "Brave-Browser"),
		c("Vivaldi", "Vivaldi"), c("Chromium", "Chromium"),
		{"Firefox", Firefox, filepath.Join(roaming, "Mozilla", "Firefox", "Profiles", "*", "places.sqlite")},
	}
}

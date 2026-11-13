package tabs

import (
	"os"
	"path/filepath"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

func sessionDirs(b browsers.Browser) []string {
	local, roaming := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA")
	switch b.Name {
	case "Google Chrome":
		return chromiumProfiles(filepath.Join(local, "Google", "Chrome", "User Data"))
	case "Chromium":
		return chromiumProfiles(filepath.Join(local, "Chromium", "User Data"))
	case "Brave Browser":
		return chromiumProfiles(filepath.Join(local, "BraveSoftware", "Brave-Browser", "User Data"))
	case "Microsoft Edge":
		return chromiumProfiles(filepath.Join(local, "Microsoft", "Edge", "User Data"))
	case "Vivaldi":
		return chromiumProfiles(filepath.Join(local, "Vivaldi", "User Data"))
	case "Opera":
		return profilesIn(filepath.Join(roaming, "Opera Software", "Opera Stable"))
	case "Firefox":
		return profilesIn(filepath.Join(roaming, "Mozilla", "Firefox", "Profiles", "*"))
	}
	return nil
}

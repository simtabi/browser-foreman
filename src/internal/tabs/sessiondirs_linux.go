package tabs

import (
	"os"
	"path/filepath"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

func sessionDirs(b browsers.Browser) []string {
	h, _ := os.UserHomeDir()
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(h, ".config")
	}
	switch b.Name {
	case "Google Chrome":
		return chromiumProfiles(filepath.Join(cfg, "google-chrome"))
	case "Chromium":
		return append(chromiumProfiles(filepath.Join(cfg, "chromium")), chromiumProfiles(filepath.Join(h, "snap", "chromium", "common", "chromium"))...)
	case "Brave Browser":
		return chromiumProfiles(filepath.Join(cfg, "BraveSoftware", "Brave-Browser"))
	case "Microsoft Edge":
		return chromiumProfiles(filepath.Join(cfg, "microsoft-edge"))
	case "Vivaldi":
		return chromiumProfiles(filepath.Join(cfg, "vivaldi"))
	case "Opera":
		return profilesIn(filepath.Join(cfg, "opera"))
	case "Firefox":
		return profilesIn(filepath.Join(h, ".mozilla", "firefox", "*"), filepath.Join(h, "snap", "firefox", "common", ".mozilla", "firefox", "*"))
	case "Zen":
		return profilesIn(filepath.Join(h, ".zen", "*"))
	}
	return nil
}

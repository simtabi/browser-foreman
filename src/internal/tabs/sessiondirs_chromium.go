//go:build linux || windows

package tabs

import "path/filepath"

// chromiumProfiles are the profile folders of a Chromium browser's user data folder. Linux and Windows find
// Chromium profiles this way; macOS reads them from the browser list instead.
func chromiumProfiles(userData string) []string {
	return profilesIn(filepath.Join(userData, "Default"), filepath.Join(userData, "Profile *"))
}

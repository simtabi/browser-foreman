//go:build !windows

package chromefile

import (
	"os"
	"path/filepath"
)

// running: on macOS, Linux and the BSDs Chromium holds a SingletonLock link in its user data folder while it runs.
func running(userData string) bool {
	_, err := os.Lstat(filepath.Join(userData, "SingletonLock"))
	return err == nil
}

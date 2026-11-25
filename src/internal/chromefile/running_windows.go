//go:build windows

package chromefile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// running: on Windows Chromium creates no SingletonLock. It holds <User Data>\lockfile open for writing, shared for
// reading only, and deletes it on exit, so opening it for writing fails while the browser runs. A lockfile that opens
// is stale (left by a crash); a missing one means nothing runs. Any other failure is read as running, because the
// caller is about to overwrite the browser's bookmarks and a wrong "not running" loses the write.
func running(userData string) bool {
	f, err := os.OpenFile(filepath.Join(userData, "lockfile"), os.O_RDWR, 0)
	if err == nil {
		f.Close()
		return false
	}
	return !errors.Is(err, fs.ErrNotExist)
}

// Package atomicfile writes a file so that it is never seen half written, and is on disk before it replaces the old
// one: a crash or a power cut leaves either the old file or the new, never an empty or torn one.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces path with b. The bytes go to a temporary file of its own in the same folder (two writers never share
// one), are synced to disk, and the file is then renamed over path, with perm.
func Write(path string, b []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

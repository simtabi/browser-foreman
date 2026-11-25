//go:build windows

package chromefile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Holds a lockfile the way Chromium does (write access, read sharing only, deleted on close) and checks Running sees
// it, then sees nothing once it is let go.
func TestRunningSeesChromiumsWindowsLockfile(t *testing.T) {
	userData := t.TempDir()
	profile := filepath.Join(userData, "Default")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if Running(profile) {
		t.Fatal("running with no lockfile")
	}
	name, _ := syscall.UTF16PtrFromString(filepath.Join(userData, "lockfile"))
	const fileFlagDeleteOnClose = 0x04000000
	h, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil, syscall.CREATE_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL|fileFlagDeleteOnClose, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !Running(profile) {
		t.Fatal("a held lockfile was not seen")
	}
	syscall.CloseHandle(h)
	if Running(profile) {
		t.Fatal("still running after the lock was let go")
	}
}

//go:build linux

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdInstallStatusUninstall(t *testing.T) {
	var calls []string
	active := false
	run := func(name string, args ...string) (string, error) {
		c := strings.Join(args, " ")
		calls = append(calls, c)
		switch {
		case strings.HasPrefix(c, "--user restart"):
			active = true
		case strings.HasPrefix(c, "--user disable"):
			active = false
		case strings.HasPrefix(c, "--user is-active"):
			if active {
				return "active\n", nil
			}
			return "inactive\n", nil
		}
		return "", nil
	}
	m := Manager{Home: t.TempDir(), Run: run}
	p, err := m.Install(spec(nil))
	if err != nil || p != filepath.Join(m.Home, ".config", "systemd", "user", unitName) {
		t.Fatalf("%s %v", p, err)
	}
	if st := m.Status(); !st.Installed || !st.Running {
		t.Fatalf("%+v %q", st, calls)
	}
	if had, err := m.Uninstall(); !had || err != nil {
		t.Fatal(had, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) || m.Status().Running {
		t.Fatal("still there")
	}
}

//go:build linux

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const unitName = "browser-foreman.service"

func (m Manager) path() string {
	if m.ConfigHome != "" {
		return filepath.Join(m.ConfigHome, "systemd", "user", unitName)
	}
	return filepath.Join(m.Home, ".config", "systemd", "user", unitName)
}

// Install writes the systemd user unit, enables it and starts it (restarting one already running).
func (m Manager) Install(s Spec) (string, error) {
	p := m.path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(Unit(s)), 0o644); err != nil {
		return "", err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", unitName}, {"--user", "restart", unitName}} {
		if out, err := m.Run("systemctl", args...); err != nil {
			return p, fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, trim(out))
		}
	}
	return p, nil
}

// Uninstall stops and disables the unit and removes its file. It reports whether there was one.
func (m Manager) Uninstall() (bool, error) {
	p := m.path()
	if _, err := os.Stat(p); err != nil {
		return false, nil
	}
	_, _ = m.Run("systemctl", "--user", "disable", "--now", unitName)
	if err := os.Remove(p); err != nil {
		return true, err
	}
	_, _ = m.Run("systemctl", "--user", "daemon-reload")
	return true, nil
}

// Status reads whether the unit is installed and active.
func (m Manager) Status() Status {
	st := Status{Path: m.path()}
	if _, err := os.Stat(st.Path); err == nil {
		st.Installed = true
	}
	out, _ := m.Run("systemctl", "--user", "is-active", unitName)
	st.Detail = trim(out)
	st.Running = st.Detail == "active"
	return st
}

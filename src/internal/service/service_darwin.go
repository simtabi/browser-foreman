//go:build darwin

package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (m Manager) path() string {
	return filepath.Join(m.Home, "Library", "LaunchAgents", Label+".plist")
}

func (m Manager) target() string { return "gui/" + strconv.Itoa(m.UID) }

// Install writes the launchd agent and starts it, replacing one already loaded.
func (m Manager) Install(s Spec) (string, error) {
	p := m.path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(s.Log), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, Plist(s), 0o644); err != nil {
		return "", err
	}
	m.bootout()
	if out, err := m.Run("launchctl", "bootstrap", m.target(), p); err != nil {
		return p, fmt.Errorf("launchctl bootstrap: %v: %s", err, trim(out))
	}
	return p, nil
}

// Uninstall stops the agent and removes its file. It reports whether there was one.
func (m Manager) Uninstall() (bool, error) {
	p := m.path()
	_, statErr := os.Stat(p)
	m.bootout()
	if statErr != nil {
		return false, nil
	}
	return true, os.Remove(p)
}

// bootout stops the loaded job, if any, and waits until launchd has let it go: bootout returns while the server is
// still shutting down, and a bootstrap before it is gone fails with "5: Input/output error".
func (m Manager) bootout() {
	if _, err := m.Run("launchctl", "bootout", m.target()+"/"+Label); err != nil {
		return // not loaded: nothing to stop
	}
	for range 100 { // up to 20 s: serve finishes what it is doing first
		if _, err := m.Run("launchctl", "print", m.target()+"/"+Label); err != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Status reads whether the agent is installed and running.
func (m Manager) Status() Status {
	st := Status{Path: m.path()}
	if _, err := os.Stat(st.Path); err == nil {
		st.Installed = true
	}
	out, err := m.Run("launchctl", "print", m.target()+"/"+Label)
	if err == nil {
		// The job's own state comes first; nested blocks (endpoints, sockets) list states of their own after it.
		for _, line := range strings.Split(out, "\n") {
			if l := strings.TrimSpace(line); strings.HasPrefix(l, "state = ") {
				st.Detail = l
				st.Running = l == "state = running"
				if l == "state = xpcproxy" || l == "state = spawn scheduled" {
					st.Detail = "starting" // launchd is launching it
				}
				break
			}
		}
	} else if st.Installed {
		st.Detail = "launchd has not loaded it"
	}
	return st
}

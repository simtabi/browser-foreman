//go:build windows

package service

import (
	"fmt"
	"strings"
)

// taskName is the logon task's name in Task Scheduler.
const taskName = "Browser Foreman"

// Install creates (or replaces) the logon task and starts it now.
func (m Manager) Install(s Spec) (string, error) {
	if out, err := m.Run("schtasks", "/Create", "/F", "/SC", "ONLOGON", "/RL", "LIMITED", "/TN", taskName, "/TR", TaskCommand(s)); err != nil {
		return taskName, fmt.Errorf("schtasks /Create: %v: %s", err, trim(out))
	}
	if out, err := m.Run("schtasks", "/Run", "/TN", taskName); err != nil {
		return taskName, fmt.Errorf("schtasks /Run: %v: %s", err, trim(out))
	}
	return taskName, nil
}

// Uninstall ends and deletes the task. It reports whether there was one.
func (m Manager) Uninstall() (bool, error) {
	if _, err := m.Run("schtasks", "/Query", "/TN", taskName); err != nil {
		return false, nil
	}
	_, _ = m.Run("schtasks", "/End", "/TN", taskName)
	if out, err := m.Run("schtasks", "/Delete", "/F", "/TN", taskName); err != nil {
		return true, fmt.Errorf("schtasks /Delete: %v: %s", err, trim(out))
	}
	return true, nil
}

// Status reads whether the task exists and runs.
func (m Manager) Status() Status {
	st := Status{Path: taskName}
	out, err := m.Run("schtasks", "/Query", "/TN", taskName, "/FO", "LIST")
	if err != nil {
		return st
	}
	st.Installed = true
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "Status" {
			st.Detail = strings.TrimSpace(v)
			st.Running = st.Detail == "Running"
		}
	}
	return st
}

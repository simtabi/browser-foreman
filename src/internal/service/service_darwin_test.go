//go:build darwin

package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLaunchd records the launchctl commands and answers print as loaded says.
type fakeLaunchd struct {
	calls  []string
	loaded bool
}

func (f *fakeLaunchd) run(name string, args ...string) (string, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch args[0] {
	case "bootstrap":
		f.loaded = true
	case "bootout":
		if !f.loaded {
			return "Boot-out failed: 3: No such process", errors.New("exit status 3")
		}
		f.loaded = false
	case "print":
		if !f.loaded {
			return "", errors.New("exit status 113")
		}
		return "gui/501/" + Label + " = {\n\tstate = running\n}", nil
	}
	return "", nil
}

func TestLaunchdInstallStatusUninstall(t *testing.T) {
	f := &fakeLaunchd{}
	m := Manager{Home: t.TempDir(), UID: 501, Run: f.run}
	if st := m.Status(); st.Installed || st.Running {
		t.Fatalf("before: %+v", st)
	}
	s := spec(nil)
	s.Log = filepath.Join(m.Home, "state", "service.log")
	p, err := m.Install(s)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(p); err != nil || !strings.Contains(string(b), "<string>--no-open</string>") {
		t.Fatalf("%s %v", b, err)
	}
	if want := "launchctl bootstrap gui/501 " + p; f.calls[len(f.calls)-1] != want {
		t.Fatalf("%q", f.calls)
	}
	if st := m.Status(); !st.Installed || !st.Running {
		t.Fatalf("after install: %+v", st)
	}
	if _, err := m.Install(s); err != nil || !f.loaded { // again: replaces the loaded one
		t.Fatalf("reinstall: %v %q", err, f.calls)
	}
	if had, err := m.Uninstall(); !had || err != nil || f.loaded {
		t.Fatalf("uninstall: %v %v", had, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("the plist was left behind")
	}
	if had, err := m.Uninstall(); had || err != nil {
		t.Fatalf("second uninstall: %v %v", had, err)
	}
}

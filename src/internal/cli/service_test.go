package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/service"
)

type fakeInstaller struct {
	got service.Spec
	err error
}

func (f *fakeInstaller) Install(s service.Spec) (string, error) {
	f.got = s
	return "/home/u/Library/LaunchAgents/" + service.Label + ".plist", f.err
}

func TestServiceInstallSaysWhatHappens(t *testing.T) {
	var out bytes.Buffer
	f := &fakeInstaller{}
	s := service.Spec{Exe: "/usr/local/bin/browser-foreman", Args: []string{"serve", "--no-open"}, Log: "/s/service.log"}
	if err := installService(&out, f, s, "127.0.0.1:8765"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"starts at login", "/usr/local/bin/browser-foreman serve --no-open", "/s/service.log", "http://127.0.0.1:8765/", "service uninstall"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %q", want, out.String())
		}
	}
	f.err = errors.New("launchctl bootstrap: exit status 5")
	if err := installService(&bytes.Buffer{}, f, s, "127.0.0.1:8765"); err == nil || !strings.Contains(err.Error(), "installing the login service") {
		t.Fatalf("%v", err)
	}
}

func TestServiceStatusReadsPlainly(t *testing.T) {
	cases := map[string]service.Status{
		"Not installed":                {},
		"Installed and running (/p)":   {Installed: true, Running: true, Path: "/p"},
		"not running: state = waiting": {Installed: true, Path: "/p", Detail: "state = waiting"},
	}
	for want, st := range cases {
		var out bytes.Buffer
		printServiceStatus(&out, st)
		if !strings.Contains(out.String(), want) {
			t.Errorf("%+v: %q", st, out.String())
		}
	}
}

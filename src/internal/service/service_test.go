package service

import (
	"strings"
	"testing"

	"howett.net/plist"
)

func spec(env map[string]string) Spec {
	return Spec{Exe: "/Applications/Browser Foreman/browser-foreman", Args: []string{"serve", "--no-open"}, Env: env, Log: "/home/u/.local/state/browser-foreman/service.log"}
}

func TestThePlistIsValidAndRunsServe(t *testing.T) {
	s := spec(map[string]string{"XDG_STATE_HOME": "/s & <t>", "BROFO_SERVER_ADDR": "127.0.0.1:9000"})
	var got struct {
		Label     string            `plist:"Label"`
		Args      []string          `plist:"ProgramArguments"`
		Env       map[string]string `plist:"EnvironmentVariables"`
		RunAtLoad bool              `plist:"RunAtLoad"`
		KeepAlive map[string]bool   `plist:"KeepAlive"`
		Out       string            `plist:"StandardOutPath"`
		Process   string            `plist:"ProcessType"`
	}
	if _, err := plist.Unmarshal(Plist(s), &got); err != nil {
		t.Fatalf("not a property list: %v\n%s", err, Plist(s))
	}
	if got.Label != Label || strings.Join(got.Args, "|") != s.Exe+"|serve|--no-open" || !got.RunAtLoad || got.Out != s.Log {
		t.Fatalf("%+v", got)
	}
	if got.Process != "Standard" {
		t.Fatalf("ProcessType %q: Background throttles the server the page waits on", got.Process)
	}
	if ka, ok := got.KeepAlive["SuccessfulExit"]; !ok || ka {
		t.Fatalf("restarts after a clean stop: %+v", got.KeepAlive)
	}
	if got.Env["XDG_STATE_HOME"] != "/s & <t>" || got.Env["BROFO_SERVER_ADDR"] != "127.0.0.1:9000" || len(got.Env) != 2 {
		t.Fatalf("environment: %+v", got.Env)
	}
}

func TestTheUnitQuotesEveryWord(t *testing.T) {
	u := Unit(spec(map[string]string{"XDG_CONFIG_HOME": `/c/50%"x"`}))
	for _, want := range []string{
		`ExecStart="/Applications/Browser Foreman/browser-foreman" "serve" "--no-open"`,
		`Environment="XDG_CONFIG_HOME=/c/50%%\"x\""`,
		"Restart=on-failure", "WantedBy=default.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %s in\n%s", want, u)
		}
	}
}

func TestTheTaskCommandCarriesTheEnvironment(t *testing.T) {
	if got := TaskCommand(spec(nil)); got != `"/Applications/Browser Foreman/browser-foreman" serve --no-open` {
		t.Fatalf("%s", got)
	}
	got := TaskCommand(spec(map[string]string{"XDG_STATE_HOME": `C:\s`}))
	if got != `cmd /c "set "XDG_STATE_HOME=C:\s" && "/Applications/Browser Foreman/browser-foreman" serve --no-open"` {
		t.Fatalf("%s", got)
	}
}

func TestNewSpecRefusesWhatCannotRunAtLogin(t *testing.T) {
	env := map[string]string{"XDG_STATE_HOME": "/state", "HOME": "/home/u", "BROFO_API_TOKEN": "x"}
	get := func(k string) string { return env[k] }
	s, err := NewSpec("/usr/local/bin/browser-foreman", "/state/browser-foreman", get)
	if err != nil || s.Log != "/state/browser-foreman/service.log" || len(s.Env) != 1 || s.Env["XDG_STATE_HOME"] != "/state" {
		t.Fatalf("%+v %v", s, err) // the token stays in .env, never in a service definition
	}
	if _, err := NewSpec("browser-foreman", "/s", get); err == nil {
		t.Error("a relative path was accepted")
	}
	if _, err := NewSpec("/var/folders/ab/T/go-build123/b001/exe/browser-foreman", "/s", get); err == nil || !strings.Contains(err.Error(), "go run") {
		t.Errorf("a go run binary was accepted: %v", err)
	}
}

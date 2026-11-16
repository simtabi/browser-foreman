// Package service runs Browser Foreman at login without a terminal: a launchd agent on macOS, a systemd user unit on
// Linux and a logon task on Windows. Each runs `browser-foreman serve --no-open`, so tab autosave and the link
// re-checks keep going with no page open. It is opt-in: nothing installs it but `browser-foreman service install`.
package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Label names the service everywhere: the launchd label, the systemd unit and the Windows task.
const Label = "com.simtabi.browser-foreman"

// Spec is what the service runs.
type Spec struct {
	Exe  string            // the browser-foreman binary, absolute
	Args []string          // serve --no-open
	Env  map[string]string // carried over from the installing shell: where the config and state live
	Log  string            // where its output goes (launchd); systemd and Windows keep their own logs
}

// Runner runs a service manager's command (launchctl, systemctl, schtasks). Tests replace it, so they never touch the
// real one.
type Runner func(name string, args ...string) (string, error)

// Manager installs, removes and reports the service for one user.
type Manager struct {
	Home string // the user's home directory
	// ConfigHome is $XDG_CONFIG_HOME, where systemd looks for user units; "" means ~/.config.
	ConfigHome string
	UID        int // the user's id (launchd's gui/<uid> domain)
	Run        Runner
}

// Status is where the service stands.
type Status struct {
	Installed bool   // its definition is in place
	Running   bool   // the service manager says it runs
	Path      string // where its definition is (a file, or the task's name on Windows)
	Detail    string // what the service manager said, trimmed
}

// carried are the variables a service needs from the installing shell so it reads the same configuration and state:
// a login service starts with almost no environment.
var carried = []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "BROFO_SERVER_ADDR", "BROFO_STORAGE_PATH"}

// NewSpec is the service for exe, carrying the configuration variables getenv has.
func NewSpec(exe, stateDir string, getenv func(string) string) (Spec, error) {
	if !filepath.IsAbs(exe) {
		return Spec{}, fmt.Errorf("%s is not an absolute path", exe)
	}
	if temporary(exe) {
		return Spec{}, fmt.Errorf("%s is a temporary build (go run): build browser-foreman and install the service from the binary", exe)
	}
	env := map[string]string{}
	for _, k := range carried {
		if v := getenv(k); v != "" {
			env[k] = v
		}
	}
	return Spec{Exe: exe, Args: []string{"serve", "--no-open"}, Env: env, Log: filepath.Join(stateDir, "service.log")}, nil
}

// temporary says whether exe is a binary `go run` or `go test` built in a temporary directory, which is gone the next
// time the service starts.
func temporary(exe string) bool {
	for _, d := range []string{os.TempDir(), "/tmp/", "/private/var/folders/", "/var/folders/"} {
		if d != "" && strings.HasPrefix(exe, strings.TrimSuffix(d, "/")+"/") && strings.Contains(exe, "go-build") {
			return true
		}
	}
	return strings.Contains(exe, string(filepath.Separator)+"go-build")
}

func (s Spec) envKeys() []string {
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func xmlEscape(v string) string {
	var b bytes.Buffer
	for _, r := range v {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Plist is the launchd agent: started at login, restarted if it stops with an error, left stopped after a clean stop
// (`browser-foreman stop`). It runs as a Standard process: Background lets macOS throttle its CPU and disk, which
// suits batch work, not a server the page waits on.
func Plist(s Spec) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range append([]string{s.Exe}, s.Args...) {
		b.WriteString("\t\t<string>" + xmlEscape(a) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	if len(s.Env) > 0 {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		for _, k := range s.envKeys() {
			b.WriteString("\t\t<key>" + xmlEscape(k) + "</key>\n\t\t<string>" + xmlEscape(s.Env[k]) + "</string>\n")
		}
		b.WriteString("\t</dict>\n")
	}
	b.WriteString(`	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ProcessType</key>
	<string>Standard</string>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(s.Log) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(s.Log) + `</string>
</dict>
</plist>
`)
	return b.Bytes()
}

// systemdQuote quotes one word of ExecStart: double quotes, with backslash, quote, and % (a specifier) escaped.
func systemdQuote(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`)
	return `"` + r.Replace(v) + `"`
}

// Unit is the systemd user unit: started with the user's session, restarted after a failure.
func Unit(s Spec) string {
	words := []string{}
	for _, a := range append([]string{s.Exe}, s.Args...) {
		words = append(words, systemdQuote(a))
	}
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=Browser Foreman: bookmarks and tabs, kept clean\n\n[Service]\n")
	b.WriteString("ExecStart=" + strings.Join(words, " ") + "\n")
	for _, k := range s.envKeys() {
		b.WriteString("Environment=" + systemdQuote(k+"="+s.Env[k]) + "\n")
	}
	b.WriteString("Restart=on-failure\nRestartSec=10\n\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// TaskCommand is the Windows logon task's command line. A task has no environment of its own, so the carried
// variables are set by cmd before the binary starts.
func TaskCommand(s Spec) string {
	q := func(v string) string { return `"` + strings.ReplaceAll(v, `"`, `""`) + `"` }
	run := q(s.Exe) + " " + strings.Join(s.Args, " ")
	if len(s.Env) == 0 {
		return run
	}
	sets := []string{}
	for _, k := range s.envKeys() {
		sets = append(sets, "set "+q(k+"="+s.Env[k]))
	}
	return `cmd /c "` + strings.Join(sets, " && ") + " && " + run + `"`
}

// ErrUnsupported is a platform with no service manager this knows.
var ErrUnsupported = errors.New("running at login is supported on macOS (launchd), Linux (systemd) and Windows (a logon task)")

func trim(out string) string {
	out = strings.TrimSpace(out)
	if len(out) > 400 {
		out = out[:400] + "…"
	}
	return out
}

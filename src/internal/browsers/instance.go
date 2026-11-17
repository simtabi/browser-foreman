package browsers

import (
	"context"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// Instance is one running browser: which app it runs from, which profile folder, and whether automation started it.
// Two can share a name: your Chrome and another Chrome (say, one an assistant drives with a profile of its own), or
// a second copy of the app in another folder. Telling them apart is what lets Browser Foreman open its setup page in
// the very browser the page runs in, instead of whichever copy the system picks.
type Instance struct {
	PID       int    `json:"pid"`
	App       string `json:"app"`               // the .app folder (macOS), or the executable
	Exe       string `json:"exe"`               // the executable
	Profile   string `json:"profile,omitempty"` // --user-data-dir, "" for the browser's own default
	Automated bool   `json:"automated"`         // started by automation (a test or assistant's browser)
}

// Locator finds running browsers; the macOS platform is one.
type Locator interface {
	// PageInstance is the browser holding the connection that comes from remoteAddr (a page calling this server).
	PageInstance(remoteAddr string) (Instance, error)
	// Running lists the running instances of b, the main process of each.
	Running(b Browser) []Instance
	// OpenIn opens url as a new tab in that running instance: its frontmost window, not a new window or another copy.
	OpenIn(ctx context.Context, in Instance, url string) error
}

var (
	appExe      = regexp.MustCompile(`^(.*?\.app)/Contents/MacOS/([^/]+?)(?: --|$)`)
	userDataDir = regexp.MustCompile(`--user-data-dir=(.+?)(?: --|$)`)
)

// automationFlags mark a browser started by automation rather than by a person.
var automationFlags = []string{"--enable-automation", "--remote-debugging-port", "--remote-debugging-pipe", "--use-mock-keychain"}

// ParseMain reads a process's command line (ps -o command) as a browser's main process: its app, executable, profile
// and whether automation started it. Helpers (renderers, the network service) are not main processes.
func ParseMain(pid int, command string) (Instance, bool) {
	m := appExe.FindStringSubmatch(command)
	if m == nil || strings.Contains(m[2], "Helper") {
		return Instance{}, false
	}
	in := Instance{PID: pid, App: m[1], Exe: m[1] + "/Contents/MacOS/" + m[2]}
	if p := userDataDir.FindStringSubmatch(command); p != nil {
		in.Profile = strings.Trim(p[1], `"'`)
	}
	for _, f := range automationFlags {
		if strings.Contains(command, f) {
			in.Automated = true
		}
	}
	return in, true
}

// ExeName is the executable's file name an app's main process runs ("Google Chrome" for Google Chrome.app).
func ExeName(app string) string {
	base := app[strings.LastIndex(app, "/")+1:]
	return strings.TrimSuffix(base, ".app")
}

// ParseLsofPIDs reads lsof -Fp output: the process ids, without self.
func ParseLsofPIDs(out string, self int) []int {
	var pids []int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "p") {
			if n, err := strconv.Atoi(line[1:]); err == nil && n != self {
				pids = append(pids, n)
			}
		}
	}
	return pids
}

// LoopbackPort is the port of a loopback remote address ("127.0.0.1:54321"), or 0 for anything else: only a page on
// this computer is looked up.
func LoopbackPort(remoteAddr string) int {
	host, port, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return 0
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}

// ParsePS reads ps -o pid=,command= lines into browser main processes whose executable is named exe ("Google Chrome").
func ParsePS(out, exe string) []Instance {
	var found []Instance
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			continue
		}
		pid, err := strconv.Atoi(line[:sp])
		if err != nil {
			continue
		}
		if in, ok := ParseMain(pid, strings.TrimSpace(line[sp+1:])); ok && ExeName(in.App) == exe {
			found = append(found, in)
		}
	}
	return found
}

// ParseParent reads one ps -o ppid=,command= line: the parent's id and the command.
func ParseParent(out string) (int, string, bool) {
	line := strings.TrimSpace(out)
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return 0, "", false
	}
	ppid, err := strconv.Atoi(line[:sp])
	if err != nil {
		return 0, "", false
	}
	return ppid, strings.TrimSpace(line[sp+1:]), true
}

// OpenArgs are the arguments that hand url to a running instance: its executable, run again with the same profile
// folder, passes the address to the copy already running (Chromium's process singleton) and exits, and that copy
// opens it as a tab in its last active window.
func OpenArgs(in Instance, url string) []string {
	argv := []string{in.Exe}
	if in.Profile != "" {
		argv = append(argv, "--user-data-dir="+in.Profile)
	}
	return append(argv, url)
}

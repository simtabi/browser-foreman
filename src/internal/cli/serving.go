package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/version"
)

// occupant is what answers on an address.
type occupant struct {
	Up      bool   // something answered over HTTP
	Ours    bool   // it is Browser Foreman
	Version string // its version ("" for a build from before health said it)
	PID     int    // its process (0 when health did not say)
}

// probe asks what answers on addr, within a second.
func probe(ctx context.Context, addr string) occupant {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/v1/health", nil)
	if err != nil {
		return occupant{}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return occupant{}
	}
	defer resp.Body.Close()
	var h struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
		Version string `json:"version"`
		PID     int    `json:"pid"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	ours := resp.StatusCode == http.StatusOK && json.Unmarshal(raw, &h) == nil && h.OK &&
		(h.Service == "browser-foreman" || (h.Service == "" && strings.TrimSpace(string(raw)) == `{"ok":true}`)) // older builds said only that
	return occupant{Up: true, Ours: ours, Version: h.Version, PID: h.PID}
}

// answers says whether a Browser Foreman server answers at addr within a second.
func answers(ctx context.Context, addr string) bool { return probe(ctx, addr).Ours }

// runtimeInfo is where the running server listens: written by serve, read by the commands that talk to it, so they
// find it on a fallback port too.
type runtimeInfo struct {
	Addr    string    `json:"addr"`
	PID     int       `json:"pid"`
	Version string    `json:"version"`
	Started time.Time `json:"started"`
}

func runtimePath(c config.Config) string { return filepath.Join(c.Storage.Path, "server.json") }

func writeRuntime(c config.Config, addr string, now time.Time) error {
	b, _ := json.MarshalIndent(runtimeInfo{Addr: addr, PID: os.Getpid(), Version: version.Version, Started: now.UTC()}, "", "  ")
	if err := os.MkdirAll(c.Storage.Path, 0o700); err != nil {
		return err
	}
	return os.WriteFile(runtimePath(c), append(b, '\n'), 0o600)
}

// removeRuntime removes the record if it is this process's.
func removeRuntime(c config.Config) {
	if r, ok := readRuntime(c); ok && r.PID == os.Getpid() {
		_ = os.Remove(runtimePath(c))
	}
}

func readRuntime(c config.Config) (runtimeInfo, bool) {
	b, err := os.ReadFile(runtimePath(c))
	if err != nil {
		return runtimeInfo{}, false
	}
	var r runtimeInfo
	return r, json.Unmarshal(b, &r) == nil && r.Addr != ""
}

// serverAddr is where the running server listens: the recorded address when Browser Foreman answers there, else the
// configured one.
func serverAddr(ctx context.Context, c config.Config) string {
	if r, ok := readRuntime(c); ok && r.Addr != c.Server.Addr && answers(ctx, r.Addr) {
		return r.Addr
	}
	return c.Server.Addr
}

// portHolder names the process listening on a TCP port, where lsof can say (macOS, Linux).
func portHolder(port string) (pid int, command string) {
	if runtime.GOOS == "windows" {
		return 0, ""
	}
	out, err := exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return 0, ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "p") && pid == 0:
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "c") && command == "":
			command = line[1:]
		}
	}
	return pid, command
}

// processCommand is a process's executable, where ps can say.
func processCommand(pid int) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// stopServer stops the Browser Foreman listening at addr, as Ctrl-C would (SIGTERM: it finishes what it is doing),
// and waits until the address is free. It signals only a process that answers as Browser Foreman and whose program
// is browser-foreman.
func stopServer(ctx context.Context, addr string, out io.Writer) error {
	o := probe(ctx, addr)
	if !o.Ours {
		return fmt.Errorf("no Browser Foreman is running at %s", addr)
	}
	pid := o.PID
	if pid == 0 {
		_, port, _ := net.SplitHostPort(addr)
		pid, _ = portHolder(port)
	}
	if pid == 0 {
		return fmt.Errorf("Browser Foreman at %s did not say which process it is: stop it where it runs (Ctrl-C)", addr)
	}
	if cmd := processCommand(pid); filepath.Base(cmd) != "browser-foreman" {
		return fmt.Errorf("process %d at %s is %q, not browser-foreman: not stopping it", pid, addr, cmd)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stopping process %d: %w", pid, err)
	}
	for range 100 {
		if l, err := net.Listen("tcp", addr); err == nil {
			l.Close()
			fmt.Fprintf(out, "Stopped %s (process %d) at %s.\n", strings.TrimSpace("Browser Foreman "+o.Version), pid, addr)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not stop within 10 seconds", pid)
}

// errAlreadyRunning means this same Browser Foreman already serves the address: nothing to start.
var errAlreadyRunning = errors.New("already running")

// bind listens on the configured address. When it is taken:
//   - by this same build of Browser Foreman: says so and returns errAlreadyRunning;
//   - by another build: replaces it with replace, else says how;
//   - by another program: names it and listens on the next free port (up to 20 above), which serve records.
func bind(ctx context.Context, c config.Config, replace bool, out io.Writer) (net.Listener, error) {
	addr := c.Server.Addr
	l, err := net.Listen("tcp", addr)
	if err == nil || !errors.Is(err, syscall.EADDRINUSE) {
		return l, err
	}
	o := probe(ctx, addr)
	if o.Ours {
		who := "Browser Foreman " + o.Version
		if o.Version == "" {
			who = "an older Browser Foreman"
		}
		if o.PID != 0 {
			who += fmt.Sprintf(" (process %d)", o.PID)
		}
		switch {
		case replace:
			if err := stopServer(ctx, addr, out); err != nil {
				return nil, err
			}
			return net.Listen("tcp", addr)
		case o.Version == version.Version:
			fmt.Fprintf(out, "%s is already running at http://%s. `browser-foreman serve --replace` restarts it.\n", who, addr)
			return nil, errAlreadyRunning
		default:
			return nil, fmt.Errorf("%s is running at http://%s; `browser-foreman serve --replace` replaces it with %s, or `browser-foreman stop` stops it", who, addr, version.Version)
		}
	}
	host, port, _ := net.SplitHostPort(addr)
	holder := "another program"
	if pid, cmd := portHolder(port); pid != 0 {
		holder = fmt.Sprintf("%s (process %d)", cmd, pid)
	}
	n, _ := strconv.Atoi(port)
	for p := n + 1; p <= n+20 && p <= 65535; p++ {
		alt := net.JoinHostPort(host, strconv.Itoa(p))
		if l, err := net.Listen("tcp", alt); err == nil {
			fmt.Fprintf(out, "Port %s is taken by %s, so Browser Foreman uses %s instead. Set server.addr to keep one: browser extensions paired with %s need pairing again.\n", port, holder, alt, addr)
			return l, nil
		}
	}
	return nil, fmt.Errorf("port %s is taken by %s, and so are the 20 after it: set server.addr (or BROFO_SERVER_ADDR)", port, holder)
}

// shouldOpen decides whether serve opens its page: --no-open and --open win, then server.open (auto: only when started
// from a terminal, so a service or a script never pops a browser up).
func shouldOpen(setting string, open, noOpen, terminal bool) bool {
	switch {
	case noOpen:
		return false
	case open:
		return true
	case setting == "always":
		return true
	case setting == "never":
		return false
	}
	return terminal
}

// showPage prints the page's link on a line of its own and, when wanted, opens it in the default browser.
func showPage(ctx context.Context, out io.Writer, link string, open bool) {
	fmt.Fprintf(out, "Open Browser Foreman: %s\n", link)
	if !open {
		return
	}
	s := &browsers.Service{Platform: browsers.NewPlatform()}
	res, err := s.Open(ctx, []string{link}, browsers.AsTabs, []string{browsers.SystemDefault})
	if err == nil {
		for _, r := range res {
			if r.Error != "" {
				err = errors.New(r.Error)
			}
		}
	}
	if err != nil {
		fmt.Fprintf(out, "Could not open it in the browser (%v): open the link above.\n", err)
	}
}

// pageLink asks the server running on addr for a link to its page. In local mode that is a one-time link, made with
// the token from .env, which signs the browser that opens it in; in accounts mode it is the page, which asks people
// to sign in.
func pageLink(ctx context.Context, c config.Config, addr string) (string, error) {
	if c.Server.Mode != config.ModeLocal {
		return "http://" + addr + "/", nil
	}
	if c.APIToken == "" {
		return "", errors.New("no API token is set: run `browser-foreman config init`")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/v1/local/links", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("Browser Foreman is not answering on %s: start it with `browser-foreman serve`", addr)
	}
	defer resp.Body.Close()
	var out struct {
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out)
	if resp.StatusCode != http.StatusCreated || out.URL == "" {
		return "", fmt.Errorf("the server on %s gave no link (%d %s): is it this computer's Browser Foreman, with the same token?", addr, resp.StatusCode, out.Error)
	}
	return out.URL, nil
}

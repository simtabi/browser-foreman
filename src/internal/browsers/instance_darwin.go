//go:build darwin

package browsers

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var _ Locator = (*Mac)(nil)

// PageInstance finds the browser behind a connection from this computer: lsof names the processes holding the
// connection's port (Chromium's network service, a helper), and their parents lead to the browser's main process.
func (m *Mac) PageInstance(remoteAddr string) (Instance, error) {
	port := LoopbackPort(remoteAddr)
	if port == 0 {
		return Instance{}, errors.New("not a connection from this computer")
	}
	// lsof exits 1 when it finds nothing; the output decides.
	out, _ := exec.Command("lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:ESTABLISHED", "-Fp").Output()
	for _, pid := range ParseLsofPIDs(string(out), os.Getpid()) {
		for p, hops := pid, 0; p > 1 && hops < 6; hops++ {
			o, err := exec.Command("ps", "-o", "ppid=,command=", "-ww", "-p", strconv.Itoa(p)).Output()
			if err != nil {
				break
			}
			ppid, command, ok := ParseParent(string(o))
			if !ok {
				break
			}
			if in, ok := ParseMain(p, command); ok {
				return in, nil
			}
			p = ppid
		}
	}
	return Instance{}, errors.New("no browser found behind that connection")
}

// Running lists every running copy of b (by its executable's name, so a copy of the app in another folder counts).
func (m *Mac) Running(b Browser) []Instance {
	if b.Path == "" {
		return nil
	}
	out, err := exec.Command("ps", "-axo", "pid=,command=", "-ww").Output()
	if err != nil {
		return nil
	}
	return ParsePS(string(out), ExeName(b.Path))
}

// OpenIn hands url to a running instance by running its own executable with its profile folder (OpenArgs). Only an
// executable inside an app bundle is run.
func (m *Mac) OpenIn(ctx context.Context, in Instance, url string) error {
	if !strings.HasPrefix(in.Exe, in.App+"/Contents/MacOS/") || !strings.HasSuffix(in.App, ".app") {
		return errors.New("not an app's executable")
	}
	if _, err := os.Stat(in.Exe); err != nil {
		return err
	}
	argv := OpenArgs(in, url)
	return m.start(ctx, exec.Command(argv[0], argv[1:]...))
}

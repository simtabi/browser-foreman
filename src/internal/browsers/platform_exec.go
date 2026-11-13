package browsers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// execPlatform is the Linux and Windows platform: browsers are executables, run directly with their own options.
type execPlatform struct {
	find       func() []Browser
	dflt       func() string
	icon       func(b Browser, size int) ([]byte, error)
	opener     func(url string) []string // the system's own way to open one link in the default browser
	LaunchWait time.Duration
}

func (p *execPlatform) Installed() []Browser                     { return p.find() }
func (p *execPlatform) DefaultBundle() string                    { return p.dflt() }
func (p *execPlatform) Icon(b Browser, size int) ([]byte, error) { return p.icon(b, size) }

// Command runs the browser itself: links as tabs, or after its new-window or private-window option.
func (p *execPlatform) Command(b Browser, how string, urls []string) ([]string, error) {
	switch how {
	case AsTabs, "":
		return append([]string{b.Path}, urls...), nil
	case NewWindow:
		return append([]string{b.Path, "--new-window"}, urls...), nil
	case Private:
		if b.PrivateFlag == "" {
			return nil, fmt.Errorf("%s cannot be asked for a private window; choose another browser for that", b.Name)
		}
		return append([]string{b.Path, b.PrivateFlag}, urls...), nil
	}
	return nil, fmt.Errorf("how must be tabs, window or incognito")
}

func (p *execPlatform) DefaultCommands(urls []string) [][]string {
	out := make([][]string, 0, len(urls))
	for _, u := range urls {
		out = append(out, p.opener(u))
	}
	return out
}

// Launch runs only an installed browser or the system's link opener, never anything else, and waits at most
// LaunchWait: long enough to catch a refusal, short enough that a browser starting from cold holds nothing up.
func (p *execPlatform) Launch(_ context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("nothing to run")
	}
	allowed := slices.ContainsFunc(p.find(), func(b Browser) bool { return b.Path == argv[0] }) || argv[0] == p.opener("x")[0]
	if !allowed {
		return errors.New("only an installed browser or the system's link opener is run")
	}
	var stderr bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return errors.New(msg)
			}
		}
		return err
	case <-time.After(p.LaunchWait):
		return nil
	}
}

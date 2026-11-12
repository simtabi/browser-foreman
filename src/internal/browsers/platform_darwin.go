//go:build darwin

package browsers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Mac is the macOS platform: apps found by their Info.plist, the default from LaunchServices, icons converted with
// sips, links handed over with open(1).
type Mac struct {
	Dirs []string // where apps are looked for
	// LaunchWait is how long Launch waits for open(1). A browser starting from cold takes seconds to take the links;
	// past this the launch is under way and counts as done.
	LaunchWait time.Duration
}

func NewPlatform() Platform {
	home, _ := os.UserHomeDir()
	return &Mac{Dirs: []string{"/Applications", "/Applications/Utilities", filepath.Join(home, "Applications"),
		"/System/Volumes/Preboot/Cryptexes/App/System/Applications"}, LaunchWait: 1500 * time.Millisecond}
}

// plist reads a property list (XML or binary) as JSON through plutil, which every macOS has.
func plist(path string, v any) error {
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

func (m *Mac) Installed() []Browser {
	byBundle := map[string]Browser{}
	for _, b := range Known {
		byBundle[b.Bundle] = b
	}
	found := map[string]string{}
	for _, d := range m.Dirs {
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if !strings.HasSuffix(e.Name(), ".app") {
				continue
			}
			app := filepath.Join(d, e.Name())
			var info struct {
				ID string `json:"CFBundleIdentifier"`
			}
			if plist(filepath.Join(app, "Contents", "Info.plist"), &info) != nil {
				continue
			}
			if _, known := byBundle[info.ID]; known && found[info.ID] == "" {
				found[info.ID] = app
			}
		}
	}
	out := []Browser{}
	for _, b := range Known {
		if p := found[b.Bundle]; p != "" {
			b.Path = p
			out = append(out, b)
		}
	}
	return out
}

func (m *Mac) DefaultBundle() string {
	home, _ := os.UserHomeDir()
	var doc struct {
		Handlers []struct {
			Scheme string `json:"LSHandlerURLScheme"`
			Role   string `json:"LSHandlerRoleAll"`
		} `json:"LSHandlers"`
	}
	if plist(filepath.Join(home, "Library/Preferences/com.apple.LaunchServices/com.apple.launchservices.secure.plist"), &doc) != nil {
		return ""
	}
	for _, h := range doc.Handlers {
		if h.Scheme == "https" {
			return h.Role
		}
	}
	return ""
}

func (m *Mac) Icon(b Browser, size int) ([]byte, error) {
	var info struct {
		Icon string `json:"CFBundleIconFile"`
	}
	if err := plist(filepath.Join(b.Path, "Contents", "Info.plist"), &info); err != nil {
		return nil, err
	}
	if info.Icon == "" {
		return nil, errors.New("no icon")
	}
	name := info.Icon
	if !strings.HasSuffix(name, ".icns") {
		name += ".icns"
	}
	src := filepath.Join(b.Path, "Contents", "Resources", name)
	tmp, err := os.MkdirTemp("", "brofo-icon")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	out := filepath.Join(tmp, "icon.png")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "sips", "-s", "format", "png", "-Z", strconv.Itoa(size), src, "--out", out).Run(); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

// Launch starts open(1) and waits at most LaunchWait: long enough to catch an immediate refusal, short enough that
// a browser starting from cold never holds the request up.
func (m *Mac) Launch(ctx context.Context, argv []string) error {
	if len(argv) == 0 || argv[0] != "open" {
		return errors.New("only open(1) is run") // the one command this ever needs
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
	case <-time.After(m.LaunchWait):
		return nil
	case <-ctx.Done():
		return nil // the request ended; the launch carries on
	}
}

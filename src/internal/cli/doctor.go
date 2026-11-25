package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/notify"
)

// checkup is what doctor found: one line per check, and whether any failed.
type checkup struct {
	out    io.Writer
	failed bool
}

func (k *checkup) ok(f string, a ...any)   { fmt.Fprintf(k.out, "ok    "+f+"\n", a...) }
func (k *checkup) warn(f string, a ...any) { fmt.Fprintf(k.out, "warn  "+f+"\n", a...) }
func (k *checkup) fail(f string, a ...any) {
	k.failed = true
	fmt.Fprintf(k.out, "fail  "+f+"\n", a...)
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the configuration, the storage folder, the running server and what it can reach",
		Long: "Each check prints ok, warn or fail, with what to do. The command fails when any check fails; a warning is a\n" +
			"feature that is off, not a fault.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k := &checkup{out: cmd.OutOrStdout()}
			p, c, err := load()
			if err != nil {
				k.fail("configuration: %v", err)
				return fmt.Errorf("doctor found a problem")
			}
			k.ok("configuration: %s (%s mode)", p.Config, c.Server.Mode)
			doctorStorage(k, c)
			if c.Server.Mode == config.ModeLocal {
				if c.APIToken == "" {
					k.fail("API token: none; run `browser-foreman config init`")
				} else {
					k.ok("API token: set")
				}
			}
			if n, err := notify.New(c, nil); err != nil {
				k.warn("desktop notifications: %v", err)
			} else if why := notify.ListenProblem(n); why != "" {
				k.warn("notification listener: %s", why)
			} else {
				k.ok("desktop notifications: %s", map[bool]string{true: "on", false: "off (notify.desktop)"}[c.Notify.Desktop])
			}
			doctorServer(cmd, k, c)
			if k.failed {
				return fmt.Errorf("doctor found a problem")
			}
			return nil
		},
	}
}

func doctorStorage(k *checkup, c config.Config) {
	if err := os.MkdirAll(c.Storage.Path, 0o700); err != nil {
		k.fail("storage: %s cannot be made: %v", c.Storage.Path, err)
		return
	}
	f, err := os.CreateTemp(c.Storage.Path, ".doctor-")
	if err != nil {
		k.fail("storage: %s is not writable: %v", c.Storage.Path, err)
		return
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	if info, err := os.Stat(c.Storage.Path); err == nil && info.Mode().Perm()&0o077 != 0 {
		k.warn("storage: %s can be read by other users (%o); chmod 700 it", c.Storage.Path, info.Mode().Perm())
		return
	}
	k.ok("storage: %s (%s driver)", filepath.Clean(c.Storage.Path), c.Storage.Driver)
}

func doctorServer(cmd *cobra.Command, k *checkup, c config.Config) {
	ctx := cmd.Context()
	addr := serverAddr(ctx, c)
	o := probe(ctx, addr)
	if !o.Ours {
		k.warn("server: not running on %s; start it with `browser-foreman serve` (or `browser-foreman service install`)", addr)
		return
	}
	k.ok("server: answering on http://%s", addr)
	if c.Server.Mode != config.ModeLocal || c.APIToken == "" {
		return
	}
	var b struct {
		Installed []struct {
			Name, Engine, Path string
		}
	}
	if err := callServer(ctx, c, http.MethodGet, "browsers", nil, &b); err != nil {
		k.fail("server API: %v (is this the token the server was started with?)", err)
		return
	}
	chromium := 0
	for _, x := range b.Installed {
		if x.Engine == "chromium" && x.Path != "" {
			chromium++
		}
	}
	k.ok("browsers: %d installed", len(b.Installed))
	if chromium == 0 {
		k.warn("page snapshots: no Chrome or Chromium browser installed to take them with")
	} else {
		k.ok("page snapshots: %d Chromium browser(s) can take them", chromium)
	}
}

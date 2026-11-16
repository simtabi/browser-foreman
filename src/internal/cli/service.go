package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/service"
)

// serviceCmd runs Browser Foreman at login, with no terminal and no page open, so tab autosave and the link re-checks
// keep going. Nothing installs it but `service install`.
func serviceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "service",
		Short: "Run Browser Foreman at login, without a terminal (launchd, systemd or a Windows logon task)",
	}
	c.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Start `browser-foreman serve` at every login, and now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cfg, err := load()
			if err != nil {
				return err
			}
			m, err := serviceManager()
			if err != nil {
				return err
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if r, err := filepath.EvalSymlinks(exe); err == nil {
				exe = r
			}
			s, err := service.NewSpec(exe, cfg.Storage.Path, os.Getenv)
			if err != nil {
				return err
			}
			return installService(cmd.OutOrStdout(), m, s, cfg.Server.Addr)
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Stop the login service and remove it (the data stays)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := serviceManager()
			if err != nil {
				return err
			}
			had, err := m.Uninstall()
			switch {
			case err != nil:
				return err
			case !had:
				fmt.Fprintln(cmd.OutOrStdout(), "No login service is installed.")
			default:
				fmt.Fprintln(cmd.OutOrStdout(), "Removed the login service. Browser Foreman no longer starts at login; your data is unchanged.")
			}
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Say whether the login service is installed and running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := serviceManager()
			if err != nil {
				return err
			}
			printServiceStatus(cmd.OutOrStdout(), m.Status())
			return nil
		},
	})
	return c
}

// serviceManager is the service manager for this user, running the real launchctl, systemctl or schtasks.
func serviceManager() (service.Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return service.Manager{}, err
	}
	return service.Manager{Home: home, ConfigHome: os.Getenv("XDG_CONFIG_HOME"), UID: os.Getuid(), Run: runTool}, nil
}

// runTool runs a service manager's command and returns what it printed.
func runTool(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// installer is a service.Manager; tests give installService a fake, so they never install anything.
type installer interface {
	Install(service.Spec) (string, error)
}

// installService installs the service and says what now happens. A server already running in a terminal keeps the
// address, so the service's own start then finds it and stops: the service takes over at the next login.
func installService(out io.Writer, m installer, s service.Spec, addr string) error {
	where, err := m.Install(s)
	if errors.Is(err, service.ErrUnsupported) {
		return err
	}
	if err != nil {
		return fmt.Errorf("installing the login service: %w", err)
	}
	fmt.Fprintf(out, "Installed: Browser Foreman now starts at login (%s), running %s serve --no-open.\n", where, s.Exe)
	if s.Log != "" {
		fmt.Fprintf(out, "Its output goes to %s.\n", s.Log)
	}
	fmt.Fprintf(out, "Open http://%s/ to use it. `browser-foreman service uninstall` removes it.\n", addr)
	fmt.Fprintln(out, "If you move or replace the binary, run `service install` again.")
	return nil
}

func printServiceStatus(out io.Writer, st service.Status) {
	switch {
	case !st.Installed:
		fmt.Fprintln(out, "Not installed: Browser Foreman runs only while `browser-foreman serve` does. `browser-foreman service install` starts it at login.")
		return
	case st.Running:
		fmt.Fprintf(out, "Installed and running (%s).\n", st.Path)
	case st.Detail == "starting":
		fmt.Fprintf(out, "Installed and starting (%s): it answers in a few seconds.\n", st.Path)
	default:
		fmt.Fprintf(out, "Installed (%s), not running", st.Path)
		if st.Detail != "" {
			fmt.Fprintf(out, ": %s", st.Detail)
		}
		fmt.Fprintln(out, ". It starts at the next login; `browser-foreman service install` starts it now.")
	}
}

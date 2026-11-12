// Package cli is the browser-foreman command tree. Commands stay thin: parse flags, call a package, print.
package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/api"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/events"
	"github.com/simtabi/browser-foreman/src/internal/version"
	"github.com/simtabi/browser-foreman/src/internal/webhooks"
)

// Execute runs the command line and returns the process exit code.
func Execute() int {
	if err := Root().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "browser-foreman:", err)
		return 1
	}
	return 0
}

// Root is the command tree, declared here one command each.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "browser-foreman",
		Short:         "Clean bookmarks, safe tabs: a local server for your browsers",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(serveCmd(), versionCmd(), configCmd())
	return root
}

func load() (config.Paths, config.Config, error) {
	p, err := config.ResolvePaths(os.Getenv)
	if err != nil {
		return p, config.Config{}, err
	}
	c, err := config.Load(p, os.Getenv)
	return p, c, err
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the server (API, events, webhooks)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, c, err := load()
			if err != nil {
				return err
			}
			bus := events.New(1000, time.Now)
			store := webhooks.NewStore(c.Storage.Path)
			srv := &api.Server{Cfg: c, Bus: bus, Hooks: store, Now: time.Now, Dispatch: &webhooks.Dispatcher{
				Store: store, Client: &http.Client{Timeout: c.Webhooks.Timeout.Duration}, Attempts: c.Webhooks.Attempts,
				Backoff: 2 * time.Second, Now: time.Now,
				Log: func(d webhooks.Delivery) {
					if !d.OK {
						fmt.Fprintf(os.Stderr, "webhook %s: event %d failed after %d attempts: %s\n", d.Subscription, d.Event, d.Attempts, d.Error)
					}
				},
			}}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			fmt.Fprintf(cmd.OutOrStdout(), "Browser Foreman %s on http://%s (config %s, state %s)\n", version.Version, c.Server.Addr, p.Config, c.Storage.Path)
			if c.APIToken == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No API token: changes through the API are refused until you run `browser-foreman config init`.")
			}
			return srv.Run(ctx)
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "browser-foreman %s (%s, %s)\n", version.Version, version.Commit, version.Date)
		},
	}
}

func configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Show or create the configuration"}
	c.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config and state directories",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := config.ResolvePaths(os.Getenv)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config  %s\nstate   %s\n", p.Config, p.State)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the configuration in effect (the API token is not printed)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cfg, err := load()
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			token := "not set"
			if cfg.APIToken != "" {
				token = "set"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "api token: %s\nread from: %v\n", token, cfg.Sources)
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Create config.json and .env (with a new API token) in the config directory; never overwrites",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := config.ResolvePaths(os.Getenv)
			if err != nil {
				return err
			}
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			wrote, err := config.Init(p, hex.EncodeToString(b))
			if err != nil {
				return err
			}
			if len(wrote) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing written: config.json and .env already exist in", p.Config)
			}
			for _, f := range wrote {
				fmt.Fprintln(cmd.OutOrStdout(), "wrote", f)
			}
			return nil
		},
	})
	return c
}

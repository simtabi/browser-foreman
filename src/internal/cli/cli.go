// Package cli is the browser-foreman command tree. Commands stay thin: parse flags, call a package, print.
package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/simtabi/browser-foreman/src/internal/ai"
	"github.com/simtabi/browser-foreman/src/internal/api"
	"github.com/simtabi/browser-foreman/src/internal/auth"
	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/events"
	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
	"github.com/simtabi/browser-foreman/src/internal/media"
	"github.com/simtabi/browser-foreman/src/internal/notify"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/tabs"
	"github.com/simtabi/browser-foreman/src/internal/version"
	"github.com/simtabi/browser-foreman/src/internal/webhooks"
)

// Execute runs the command line and returns the process exit code.
func Execute() int {
	if err := Root().Execute(); err != nil {
		if !errors.Is(err, errNotifyFailed) { // go-notifykit has already said what went wrong
			fmt.Fprintln(os.Stderr, "browser-foreman:", err)
		}
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
	root.AddCommand(serveCmd(), versionCmd(), configCmd(), importCmd(), workspaceCmd(), storageCmd(), userCmd(), tokenCmd(), backupCmd(), restoreCmd(), pairCmd(), mcpCmd(), mergeCmd(), sortCmd(), applyCmd(), stopCmd(), serviceCmd(), notifyCmd(), aiCmd(), openCmd(), pageDetailsCmd(), webhookCmd(), doctorCmd())
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
	var replace, open, noOpen bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the server (API, events, webhooks)",
		Long: "Runs the server on server.addr. If Browser Foreman already runs there it says so and exits (--replace restarts it,\n" +
			"as `browser-foreman stop` stops it); if another program holds the port, it names it and uses the next free one,\n" +
			"which it records so the other commands find it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, c, err := load()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			terminal := term.IsTerminal(int(os.Stdout.Fd()))
			l, err := bind(ctx, c, replace, cmd.OutOrStdout())
			if errors.Is(err, errAlreadyRunning) {
				if link, err := pageLink(ctx, c, c.Server.Addr); err == nil {
					showPage(ctx, cmd.OutOrStdout(), link, shouldOpen(c.Server.Open, open, noOpen, terminal))
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "Open it with `browser-foreman open` (%v).\n", err)
				}
				return nil
			}
			if err != nil {
				return err
			}
			defer l.Close()
			c.Server.Addr = l.Addr().String()
			data, err := store.Open(c.Storage.Driver, c.Storage.Path)
			if err != nil {
				return err
			}
			defer data.Close()
			srv := newServer(c, data, time.Now)
			srv.ConfigPaths, srv.Getenv = p, os.Getenv
			srv.Listener = l
			srv.Console = cmd.OutOrStdout()
			if err := writeRuntime(c, c.Server.Addr, time.Now()); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "recording the address: %v\n", err)
			}
			defer removeRuntime(c)
			fmt.Fprintf(cmd.OutOrStdout(), "Browser Foreman %s on http://%s (config %s, state %s)\n", version.Version, c.Server.Addr, p.Config, c.Storage.Path)
			if c.Server.Mode == config.ModeLocal && c.APIToken == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No API token: the API is closed until you run `browser-foreman config init`.")
			}
			if c.Server.Mode == config.ModeAccounts {
				if users, err := data.Users(cmd.Context()); err == nil && len(users) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Accounts mode with no users: nobody can sign in until you run `browser-foreman user add NAME --role owner`.")
				}
			}
			if n, err := notify.New(c, cmd.ErrOrStderr()); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "desktop notifications are off: %v\n", err)
			} else {
				srv.Notifier = n
				if why := notify.ListenProblem(n); why != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "notification listener off: %s\n", why)
				} else {
					go func() { // so `browser-foreman notify ping` and scripts can show a notification through this server
						if err := n.Listen(ctx); err != nil && ctx.Err() == nil {
							fmt.Fprintf(cmd.ErrOrStderr(), "notification listener: %v\n", err)
						}
					}()
				}
			}
			srv.Background(ctx)
			go func() { // once it answers, so the page loads at once
				for range 50 {
					if answers(ctx, c.Server.Addr) {
						link := "http://" + c.Server.Addr + "/"
						if c.Server.Mode == config.ModeLocal {
							link = srv.LocalLink() // works once, within two minutes: signs in the browser that opens it
						}
						showPage(ctx, cmd.OutOrStdout(), link, shouldOpen(c.Server.Open, open, noOpen, terminal))
						return
					}
					time.Sleep(100 * time.Millisecond)
				}
			}()
			return srv.Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&replace, "replace", false, "stop the Browser Foreman already running on the address and take its place")
	cmd.Flags().BoolVar(&open, "open", false, "open the page in the default browser (server.open: auto opens it when started from a terminal)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "do not open the page in the browser")
	return cmd
}

func openCmd() *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the page in your browser, signed in",
		Long: "Asks the running server for a one-time link and opens it in the default browser. In local mode the page\n" +
			"opens only through such a link, so no other program or person on this computer can act as you; the link works\n" +
			"once, within two minutes. --print shows it instead of opening it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, c, err := load()
			if err != nil {
				return err
			}
			link, err := pageLink(cmd.Context(), c, serverAddr(cmd.Context(), c))
			if err != nil {
				return err
			}
			showPage(cmd.Context(), cmd.OutOrStdout(), link, !printOnly)
			return nil
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the link instead of opening it")
	return cmd
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running server (as Ctrl-C in its terminal would)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, c, err := load()
			if err != nil {
				return err
			}
			ctx := context.Background()
			return stopServer(ctx, serverAddr(ctx, c), cmd.OutOrStdout())
		},
	}
}

// newServer assembles the API server on an open store, as `serve` runs it (and `mcp` when no server is running).
func newServer(c config.Config, data store.Store, now func() time.Time) *api.Server {
	bus := events.New(1000, now)
	hooks := webhooks.NewStore(c.Storage.Path)
	deliveries := webhooks.NewLog(c.Storage.Path)
	bs := &browsers.Service{Platform: browsers.NewPlatform()}
	tabReader := tabs.NewMac()
	srv := &api.Server{Cfg: c, Bus: bus, Hooks: hooks, Data: data, Now: now, Deliveries: deliveries,
		Limit:    auth.NewLimiter(5, 15*time.Minute, 15*time.Minute),
		Checker:  linkcheck.New(15 * time.Second),
		GitHub:   linkcheck.GhCLI(),
		Browsers: bs,
		Media:    media.Store{Dir: filepath.Join(c.Storage.Path, "media")},
		Dispatch: &webhooks.Dispatcher{
			Store: hooks, Client: webhooks.NewClient(c.Webhooks.Timeout.Duration), Attempts: c.Webhooks.Attempts,
			Backoff: 2 * time.Second, Now: now,
			Log: func(d webhooks.Delivery) {
				if err := deliveries.Add(d); err != nil {
					fmt.Fprintf(os.Stderr, "webhook delivery log: %v\n", err)
				}
				if !d.OK {
					fmt.Fprintf(os.Stderr, "webhook %s: event %d failed after %d attempts: %s\n", d.Subscription, d.Event, d.Attempts, d.Error)
				}
			},
		}}
	srv.Tabs = &tabs.Service{Data: data, Browsers: bs, Reader: tabReader, Closer: tabReader, Now: now}
	srv.AI, srv.NewAI = aiProvider(c.AI), aiProvider
	return srv
}

// aiProvider is who answers AI jobs for a (validated) AI configuration; nil, for none, leaves them for an MCP client.
func aiProvider(c config.AI) ai.Provider {
	switch c.Provider {
	case "anthropic":
		return ai.NewAnthropic(c.AnthropicKey, c.Model, c.Effort)
	case "ollama":
		return &ai.Ollama{URL: c.OllamaURL, Model: c.Model}
	case "openai":
		return &ai.OpenAI{URL: c.OpenAIURL, Key: c.OpenAIKey, Model: c.Model}
	}
	return nil
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

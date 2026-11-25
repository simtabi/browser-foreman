package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/config"
)

// callServer sends one request to the running server's API as the owner (the API token) and decodes its answer into
// out (nil: discarded). body, when not nil, is sent as JSON.
func callServer(ctx context.Context, c config.Config, method, path string, body, out any) error {
	if c.Server.Mode != config.ModeLocal || c.APIToken == "" {
		return errors.New("this needs local mode and the API token (browser-foreman config init); in accounts mode use the web interface")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	addr := serverAddr(ctx, c)
	req, err := http.NewRequestWithContext(ctx, method, "http://"+addr+"/api/v1/"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("the server is not answering on %s: start it with `browser-foreman serve`", addr)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("the server answered %d: %s", resp.StatusCode, e.Error)
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

type hook struct {
	ID      string    `json:"id"`
	URL     string    `json:"url"`
	Events  []string  `json:"events"`
	Created time.Time `json:"created"`
	Active  bool      `json:"active"`
}

func webhookCmd() *cobra.Command {
	c := &cobra.Command{Use: "webhook", Short: "List, add, remove and test webhooks on the running server"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "Every webhook: id, events and address (never its secret)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cfg, err := load()
			if err != nil {
				return err
			}
			var out struct{ Webhooks []hook }
			if err := callServer(cmd.Context(), cfg, http.MethodGet, "webhooks", nil, &out); err != nil {
				return err
			}
			if len(out.Webhooks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No webhooks. Add one with `browser-foreman webhook add URL`.")
				return nil
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tACTIVE\tEVENTS\tURL")
			for _, h := range out.Webhooks {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.ID, map[bool]string{true: "yes", false: "no"}[h.Active], strings.Join(h.Events, ","), h.URL)
			}
			return tw.Flush()
		},
	})
	var events string
	add := &cobra.Command{
		Use:   "add URL",
		Short: "Add a webhook; its secret is BROFO_WEBHOOK_SECRET, or one made and shown once",
		Long: "Adds a webhook for --events (default every event). The secret signs each delivery. It is taken from\n" +
			"BROFO_WEBHOOK_SECRET when set; otherwise one is made and printed once: give it to the receiver, since the\n" +
			"server never shows it again.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := load()
			if err != nil {
				return err
			}
			secret, made := os.Getenv("BROFO_WEBHOOK_SECRET"), false
			if secret == "" {
				b := make([]byte, 32)
				_, _ = rand.Read(b)
				secret, made = hex.EncodeToString(b), true
			}
			var h hook
			body := map[string]any{"url": args[0], "events": strings.Split(events, ","), "secret": secret}
			if err := callServer(cmd.Context(), cfg, http.MethodPost, "webhooks", body, &h); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added %s for %s\n", h.ID, strings.Join(h.Events, ","))
			if made {
				fmt.Fprintf(cmd.OutOrStdout(), "secret (shown once; the receiver checks signatures with it): %s\n", secret)
			}
			return nil
		},
	}
	add.Flags().StringVar(&events, "events", "*", "comma-separated event types or families (bookmark.*), or * for all")
	c.AddCommand(add)
	c.AddCommand(&cobra.Command{
		Use: "remove ID", Short: "Remove a webhook", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := load()
			if err != nil {
				return err
			}
			if err := callServer(cmd.Context(), cfg, http.MethodDelete, "webhooks/"+args[0], nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "test ID", Short: "Send the webhook a webhook.test event now and say how it answered", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := load()
			if err != nil {
				return err
			}
			var d struct {
				OK       bool
				Status   int
				Attempts int
				Error    string
			}
			if err := callServer(cmd.Context(), cfg, http.MethodPost, "webhooks/"+args[0]+"/test", nil, &d); err != nil {
				return err
			}
			if !d.OK {
				return fmt.Errorf("not delivered after %d attempt(s): status %d %s", d.Attempts, d.Status, d.Error)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "delivered: the receiver answered %d\n", d.Status)
			return nil
		},
	})
	return c
}

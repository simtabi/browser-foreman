package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/config"
)

// pairCmd asks the running server for a one-time code to type into the browser extension. Codes live in the server
// while it runs, so this talks to it, with the local API token.
func pairCmd() *cobra.Command {
	return &cobra.Command{
		Use: "pair", Short: "Make a one-time code to pair a browser extension with the running server (local mode)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, c, err := load()
			if err != nil {
				return err
			}
			if c.Server.Mode != config.ModeLocal || c.APIToken == "" {
				return errors.New("in accounts mode, make the code in the web interface (Settings › Browser extension), signed in as an owner")
			}
			req, _ := http.NewRequest(http.MethodPost, "http://"+c.Server.Addr+"/api/v1/extension/pairings", nil)
			req.Header.Set("Authorization", "Bearer "+c.APIToken)
			resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
			if err != nil {
				return fmt.Errorf("the server is not answering on %s: start it with `browser-foreman serve`", c.Server.Addr)
			}
			defer resp.Body.Close()
			var out struct {
				Code    string    `json:"code"`
				Expires time.Time `json:"expires"`
				Error   string    `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusCreated {
				return fmt.Errorf("the server answered %d: %s", resp.StatusCode, out.Error)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pairing code: %s\nType it into the extension's popup, with the server address http://%s.\nIt works once, until %s.\n",
				out.Code, c.Server.Addr, out.Expires.Local().Format("15:04"))
			return nil
		},
	}
}

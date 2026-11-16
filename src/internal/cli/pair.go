package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/simtabi/browser-foreman/src/internal/config"
)

// pairCmd connects a browser extension with the running server, in local mode: it makes a one-time code to type into
// the extension's popup, or, with --approve, approves an extension that pressed Connect, once its four letters are
// typed (or given with --match). Pairing lives in the server while it runs, so this talks to it with the API token.
func pairCmd() *cobra.Command {
	var approve bool
	var match string
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Connect a browser extension: make a pairing code, or approve one that pressed Connect (--approve)",
		Long: "Without flags, makes a one-time code to type into the extension's popup. With --approve, lists the extensions\n" +
			"waiting to connect and approves the one whose popup shows the letters you type (or pass with --match).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, c, err := load()
			if err != nil {
				return err
			}
			if c.Server.Mode != config.ModeLocal || c.APIToken == "" {
				return errors.New("in accounts mode, connect it in the web interface (Browser extensions), signed in as an owner")
			}
			ctx := context.Background()
			c.Server.Addr = serverAddr(ctx, c) // a fallback port, when serve had to use one
			call := func(method, path string, out any) (int, error) {
				req, _ := http.NewRequestWithContext(ctx, method, "http://"+c.Server.Addr+"/api/v1/"+path, nil)
				req.Header.Set("Authorization", "Bearer "+c.APIToken)
				resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
				if err != nil {
					return 0, fmt.Errorf("the server is not answering on %s: start it with `browser-foreman serve`", c.Server.Addr)
				}
				defer resp.Body.Close()
				raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				if resp.StatusCode >= 300 {
					var e struct{ Error string }
					_ = json.Unmarshal(raw, &e)
					return resp.StatusCode, fmt.Errorf("the server answered %d: %s", resp.StatusCode, e.Error)
				}
				return resp.StatusCode, json.Unmarshal(raw, out)
			}
			out := cmd.OutOrStdout()
			if approve || match != "" {
				interactive := cmd.InOrStdin() == os.Stdin && term.IsTerminal(int(os.Stdin.Fd()))
				return approveRequest(call, cmd.InOrStdin(), out, interactive, match)
			}
			var code struct {
				Code    string    `json:"code"`
				Expires time.Time `json:"expires"`
			}
			if _, err := call(http.MethodPost, "extension/pairings", &code); err != nil {
				return err
			}
			fmt.Fprintf(out, "Pairing code: %s\nType it into the extension's popup, with the server address http://%s.\nIt works once, until %s.\n",
				code.Code, c.Server.Addr, code.Expires.Local().Format("15:04"))
			return nil
		},
	}
	cmd.Flags().BoolVar(&approve, "approve", false, "approve an extension waiting to connect, after comparing its letters")
	cmd.Flags().StringVar(&match, "match", "", "with --approve: the four letters the extension's popup shows")
	return cmd
}

type waiting struct {
	ID      string    `json:"id"`
	Browser string    `json:"browser"`
	Version string    `json:"version"`
	Match   string    `json:"match"`
	Expires time.Time `json:"expires"`
}

// approveRequest approves the waiting request whose letters are match, or asks for them on a terminal. A request is
// approved only when the letters typed are its own: that comparison is what proves the request is the owner's.
func approveRequest(call func(method, path string, out any) (int, error), in io.Reader, out io.Writer, interactive bool, match string) error {
	var list struct{ Requests []waiting }
	if _, err := call(http.MethodGet, "extension/requests", &list); err != nil {
		return err
	}
	if len(list.Requests) == 0 {
		return errors.New("no extension is waiting to connect: press Connect in its popup first")
	}
	for _, q := range list.Requests {
		fmt.Fprintf(out, "%s's extension %s asks to connect (until %s).\n", q.Browser, q.Version, q.Expires.Local().Format("15:04"))
	}
	if match == "" {
		if !interactive {
			return errors.New("pass --match with the four letters the extension's popup shows")
		}
		fmt.Fprint(out, "Type the four letters its popup shows: ")
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		match = line
	}
	match = strings.ToUpper(strings.TrimSpace(match))
	for _, q := range list.Requests {
		if strings.EqualFold(q.Match, match) {
			if _, err := call(http.MethodPost, "extension/requests/"+url.PathEscape(q.ID)+"/approve", &struct{}{}); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s is connected.\n", q.Browser)
			return nil
		}
	}
	return fmt.Errorf("no waiting extension shows %q: nothing approved", match)
}

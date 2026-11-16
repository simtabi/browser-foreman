package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/mcpserver"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/version"
)

// mcpCmd serves the Model Context Protocol on stdio, for the AI assistant that starts it. It talks to the running
// server with a token or, when none answers, to the store in-process, so it is off until the configuration turns it on
// (mcp.enabled), and read-only until writes are turned on too (mcp.writes).
func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use: "mcp", Short: "Serve bookmarks and tabs to an AI assistant over MCP, on stdio (off unless mcp.enabled)",
		Long: "Started by an AI assistant (Claude Code, Claude Desktop…) as an MCP server. It reads and changes workspaces through the\n" +
			"running server; when none answers at the configured address, it opens the store itself and runs the same API in-process,\n" +
			"on no port. Off unless mcp.enabled (or BROFO_MCP=1); the tools that change bookmarks appear only with mcp.writes (or\n" +
			"BROFO_MCP_WRITES=1). For a running server the token is BROFO_TOKEN, or in local mode the configured API token.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, c, err := load()
			if err != nil {
				return err
			}
			if !c.MCP.Enabled {
				return errors.New("MCP is off: set \"mcp\": {\"enabled\": true} in the configuration, or BROFO_MCP=1")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			var a *mcpserver.API
			if addr := serverAddr(ctx, c); answers(ctx, addr) {
				c.Server.Addr = addr
				token := os.Getenv("BROFO_TOKEN")
				if token == "" && c.Server.Mode == config.ModeLocal {
					token = c.APIToken
				}
				if token == "" {
					return errors.New("no token: make one with `browser-foreman token create` and pass it as BROFO_TOKEN")
				}
				a = mcpserver.NewAPI("http://"+c.Server.Addr, token)
			} else {
				data, err := store.Open(c.Storage.Driver, c.Storage.Path)
				if err != nil {
					return err
				}
				defer data.Close()
				// Reachable only from this process: local mode, with a token made now and kept in memory.
				b := make([]byte, 32)
				if _, err := rand.Read(b); err != nil {
					return err
				}
				token := hex.EncodeToString(b)
				c.Server.Mode, c.APIToken = config.ModeLocal, token
				a = mcpserver.InProcess(newServer(c, data, time.Now).Handler(), c.Server.Addr, token)
				fmt.Fprintf(os.Stderr, "browser-foreman mcp: no server at %s, so the store at %s is served in-process\n", c.Server.Addr, c.Storage.Path)
			}
			s := mcpserver.New(a, mcpserver.Options{Writes: c.MCP.Writes, Version: version.Version})
			return s.Run(ctx, &mcp.StdioTransport{})
		},
	}
}

package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/mcpserver"
	"github.com/simtabi/browser-foreman/src/internal/version"
)

// mcpCmd serves the Model Context Protocol on stdio, for the AI assistant that starts it. It talks to the running
// server with a token, so it is off until the configuration turns it on (mcp.enabled), and read-only until writes are
// turned on too (mcp.writes).
func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use: "mcp", Short: "Serve bookmarks and tabs to an AI assistant over MCP, on stdio (off unless mcp.enabled)",
		Long: "Started by an AI assistant (Claude Code, Claude Desktop…) as an MCP server. It reads and changes workspaces through the\n" +
			"running server, so `browser-foreman serve` must be running. Off unless mcp.enabled (or BROFO_MCP=1); the tools that\n" +
			"change bookmarks appear only with mcp.writes (or BROFO_MCP_WRITES=1). The token is BROFO_TOKEN, or in local mode the\n" +
			"configured API token.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, c, err := load()
			if err != nil {
				return err
			}
			if !c.MCP.Enabled {
				return errors.New("MCP is off: set \"mcp\": {\"enabled\": true} in the configuration, or BROFO_MCP=1")
			}
			token := os.Getenv("BROFO_TOKEN")
			if token == "" && c.Server.Mode == config.ModeLocal {
				token = c.APIToken
			}
			if token == "" {
				return errors.New("no token: make one with `browser-foreman token create` and pass it as BROFO_TOKEN")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			s := mcpserver.New(mcpserver.NewAPI("http://"+c.Server.Addr, token), mcpserver.Options{Writes: c.MCP.Writes, Version: version.Version})
			return s.Run(ctx, &mcp.StdioTransport{})
		},
	}
}

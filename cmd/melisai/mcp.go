package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/dmitriimaksimovdevelop/melisai/internal/mcp"
	"github.com/spf13/cobra"
)

// mcpCmd represents the mcp command
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start Model Context Protocol (MCP) server",
	Long: `Starts a JSON-RPC server implementing the Model Context Protocol (MCP).
This allows AI agents (e.g., Claude Desktop, Cursor) to interactively
control melisai to diagnose system issues.

Communication happens over standard input/output (stdio).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		srv := mcp.NewServer(version)
		return srv.Start(ctx)
	},
}

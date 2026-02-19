package mcp

import (
	"context"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the MCP server instance.
type Server struct {
	mcpServer *server.MCPServer
}

// NewServer creates a new MCP server with registered tools.
func NewServer(version string) *Server {
	// Create MCP server
	s := server.NewMCPServer("melisai", version, server.WithLogging())

	// Register tools
	registerTools(s)

	return &Server{
		mcpServer: s,
	}
}

// Start runs the server in stdio mode (blocking).
func (s *Server) Start(ctx context.Context) error {
	// NewStdioServer creates a wrapper that handles stdio communication
	stdioServer := server.NewStdioServer(s.mcpServer)
	return stdioServer.Listen(ctx, os.Stdin, os.Stdout)
}

// registerTools adds all supported tools to the server.
func registerTools(s *server.MCPServer) {
	// Tool: get_health
	healthTool := mcp.NewTool("get_health",
		mcp.WithDescription("Quick system health check (Tier 1 metrics only). Returns 0-100 score, anomalies, and load average. Fast (~1s). No root required."),
	)
	s.AddTool(healthTool, handleGetHealth)

	// Tool: collect_metrics
	collectTool := mcp.NewTool("collect_metrics",
		mcp.WithDescription("Run full performance profile (Tier 1 + Tier 2/3 BCC/eBPF). Returns complete JSON report with AI analysis context. Requires root for Tier 2/3."),
		mcp.WithString("profile",
			mcp.Description("Analysis depth: quick (10s, basic), standard (30s, all tools), deep (60s, + memleak/stacks)"),
			mcp.DefaultString("quick"),
			mcp.Enum("quick", "standard", "deep"),
		),
		mcp.WithString("focus",
			mcp.Description("Subsystem to focus on: disk, network, stacks, or omit for all"),
		),
		mcp.WithNumber("pid",
			mcp.Description("Target PID for process-specific analysis"),
		),
	)
	s.AddTool(collectTool, handleCollectMetrics)

	// Tool: explain_anomaly
	explainTool := mcp.NewTool("explain_anomaly",
		mcp.WithDescription("Get detailed explanation, root causes, and actionable recommendations for a specific anomaly metric. Use list_anomalies to discover available IDs."),
		mcp.WithString("anomaly_id",
			mcp.Required(),
			mcp.Description("Anomaly metric ID (e.g., 'cpu_utilization', 'disk_latency'). Use list_anomalies to see all."),
		),
	)
	s.AddTool(explainTool, handleExplainAnomaly)

	// Tool: list_anomalies
	listTool := mcp.NewTool("list_anomalies",
		mcp.WithDescription("List all known anomaly metric IDs with brief descriptions. Use with explain_anomaly to get detailed recommendations."),
	)
	s.AddTool(listTool, handleListAnomalies)
}

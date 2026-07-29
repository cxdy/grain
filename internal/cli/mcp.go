package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cxdy/grain/internal/config"
	grainmcp "github.com/cxdy/grain/internal/mcp"
	"github.com/cxdy/grain/internal/observability"
	"github.com/spf13/cobra"
)

// cmdMCP runs the MCP tool server from the main grain binary.
// Default transport is stdio (Claude Code / Codex). --http serves Streamable HTTP.
func cmdMCP(cfgPath *string, version string) *cobra.Command {
	var httpMode bool
	var listen string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the grain MCP server (stdio or Streamable HTTP)",
		Long: `Expose grain sandbox tools over the Model Context Protocol.

Transports:
  grain mcp              # stdio (default) — spawn from IDE MCP config
  grain mcp --http       # Streamable HTTP on mcp.listen (default 127.0.0.1:7476/mcp)

Requires a running daemon (grain up). Prefer co-locating HTTP MCP with the daemon:

  grain up --mcp
  # or config:
  # mcp:
  #   enabled: true
  #   listen: 127.0.0.1:7476
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				cfg = config.Defaults()
			}
			if listen != "" {
				cfg.MCP.Listen = listen
			}
			if cfg.MCP.Listen == "" {
				cfg.MCP.Listen = grainmcp.DefaultListen
			}

			// Public client package (same socket/API env as CLI).
			token := os.Getenv("GRAIN_TOKEN")
			if token == "" {
				token = cfg.ResolvedAPIToken()
			}
			c, err := grainmcp.Dial(grainmcp.ConnectOptions{
				APIURL: effectiveAPIURL(cfg),
				Token:  token,
				Socket: cfg.Socket,
			})
			if err != nil {
				return fmt.Errorf("connect to grain daemon (is it up?): %w", err)
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if httpMode {
				log := observability.NewLogger(cfg.LogLevel)
				fmt.Fprintf(os.Stderr, "grain mcp  %s\n", grainmcp.HTTPEndpoint(cfg.MCP.Listen))
				return grainmcp.RunHTTP(ctx, cfg.MCP.Listen, version, c, cfg.DataDir, log)
			}
			return grainmcp.RunStdio(ctx, version, c, cfg.DataDir)
		},
	}
	cmd.Flags().BoolVar(&httpMode, "http", false, "serve Streamable HTTP instead of stdio")
	cmd.Flags().StringVar(&listen, "listen", "", "HTTP listen address (default mcp.listen / 127.0.0.1:7476)")
	return cmd
}

// Command grain-mcp is an MCP (Model Context Protocol) server for the grain daemon.
//
// It speaks MCP over stdio so hosts like Claude Code, Codex, OpenCode, and Grok Build
// can create and manage sandboxes via tools that call the grain HTTP/unix API.
//
// Prerequisites: a running grain daemon (`grain up`).
//
// Connection (same idea as the CLI):
//
//	GRAIN_API=http://127.0.0.1:7474   # optional remote/TCP base URL
//	GRAIN_TOKEN=…                     # when the daemon requires Bearer auth
//	GRAIN_SOCKET=~/.grain/grain.sock  # default local path when GRAIN_API is unset
//
//	go run ./cmd/grain-mcp
//	# or: just build-mcp && ./bin/grain-mcp
package main

import (
	"context"
	"fmt"
	"os"

	grainmcp "github.com/cxdy/grain/internal/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Injected via -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "grain-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	c, err := grainmcp.Dial(grainmcp.ConnectFromEnv())
	if err != nil {
		return err
	}
	srv := grainmcp.NewMCPServer(version, c)
	// Stdio is the default transport for local IDE / agent hosts.
	return srv.Run(context.Background(), &mcp.StdioTransport{})
}

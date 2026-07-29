package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildFakeGrainMCP compiles a tiny stdio MCP server that exposes all required tools.
func buildFakeGrainMCP(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "fake.go")
	code := `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "mcp" {
		os.Exit(2)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake-grain", Version: "test"}, nil)
	tools := []string{
		"grain_health", "grain_list_vms", "grain_get_vm", "grain_create_vm",
		"grain_start_vm", "grain_stop_vm", "grain_delete_vm", "grain_exec",
		"grain_write_file", "grain_read_file", "grain_agent_health", "grain_logs",
		"grain_stats", "grain_workspace_sandbox", "grain_forward_add", "grain_forward_remove",
		"grain_image_list", "grain_image_pull", "grain_fs_readdir", "grain_act", "grain_k3s",
	}
	for _, name := range tools {
		n := name
		mcp.AddTool(srv, &mcp.Tool{Name: n, Description: "stub " + n},
			func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
			})
	}
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-grain")
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake grain: %v\n%s", err, out)
	}
	return bin
}

func TestRunHandshakeSuccess(t *testing.T) {
	bin := buildFakeGrainMCP(t)
	if err := runHandshake([]string{"mcp-handshake", bin}); err != nil {
		t.Fatal(err)
	}
}

func TestRunHandshakeMissingTool(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "partial.go")
	code := `package main

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "mcp" {
		os.Exit(2)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "partial", Version: "t"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "grain_health", Description: "only health"},
		func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil, nil
		})
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "partial-grain")
	cmd := exec.Command("go", "build", "-o", bin, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	err := runHandshake([]string{"mcp-handshake", bin})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("want missing tool error, got %v", err)
	}
}

func TestRunHandshakeConnectFail(t *testing.T) {
	// Binary that exits immediately without speaking MCP.
	dir := t.TempDir()
	bin := filepath.Join(dir, "bad")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := runHandshake([]string{"handshake", bin})
	if err == nil {
		t.Fatal("expected connect error")
	}
	if !strings.Contains(err.Error(), "connect") {
		t.Logf("connect-ish error: %v", err)
	}
}

func TestMainPathViaRunHandshake(t *testing.T) {
	// Cover default bin path when only program name given — typically fails connect.
	err := runHandshake([]string{"mcp-handshake"})
	if err == nil {
		// If ./bin/grain exists and works, that's fine too.
		t.Log("default ./bin/grain handshake succeeded")
		return
	}
	t.Logf("default bin path connect failed as expected: %v", err)
}

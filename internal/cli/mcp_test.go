package cli

import (
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestCmdMCPRegistered(t *testing.T) {
	t.Parallel()
	cfg := ""
	cmd := cmdMCP(&cfg, "0.0.0-test")
	if cmd.Use != "mcp" {
		t.Fatal(cmd.Use)
	}
	if cmd.Flags().Lookup("http") == nil || cmd.Flags().Lookup("listen") == nil {
		t.Fatal("flags")
	}
}

func TestPrintDaemonUpIncludesMCP(t *testing.T) {
	cfg := config.Defaults()
	cfg.MCP.Enabled = true
	cfg.MCP.Listen = "127.0.0.1:7476"
	// smoke: does not panic
	printDaemonUp("grain up  pid=1", cfg)
}

func TestCmdUpHasMCPFlag(t *testing.T) {
	cfg := ""
	cmd := cmdUp(&cfg)
	if cmd.Flags().Lookup("mcp") == nil {
		t.Fatal("missing --mcp")
	}
}

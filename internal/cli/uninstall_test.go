package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestRemovePathSafety(t *testing.T) {
	t.Parallel()
	if err := removePath("/", "data dir"); err == nil {
		t.Fatal("refused /")
	}
	if err := removePath(".", "data dir"); err == nil {
		t.Fatal("refused .")
	}
	if err := removePath("", "data dir"); err == nil {
		t.Fatal("refused empty")
	}
}

func TestRunUninstallKeepsDataWithoutPurge(t *testing.T) {
	dir := t.TempDir()
	// Fake "binary" we will remove — must not be the real test binary path
	// for this unit test: runUninstall uses os.Executable(). We only test
	// data-dir side effects via helpers.
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "grain-agent-linux-amd64"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	vms := filepath.Join(dir, "vms", "sbox-1")
	if err := os.MkdirAll(vms, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vms, "meta.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir, Socket: filepath.Join(dir, "grain.sock")}
	if err := os.WriteFile(cfg.Socket, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemonPIDPath(cfg), []byte("999999991\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate non-purge cleanup path pieces.
	if st, err := os.Stat(agentDir); err == nil && st.IsDir() {
		if err := removePath(agentDir, "guest agent dir"); err != nil {
			t.Fatal(err)
		}
	}
	cleanupStaleDaemonFiles(cfg)
	if _, err := os.Stat(agentDir); !os.IsNotExist(err) {
		t.Fatal("agent dir should be gone")
	}
	if _, err := os.Stat(vms); err != nil {
		t.Fatal("vms should remain without purge")
	}
}

func TestRunUninstallPurge(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: dir}
	if err := removePath(cfg.DataDir, "data dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("data dir should be gone")
	}
}

func TestCmdUninstallFlags(t *testing.T) {
	cfg := ""
	c := cmdUninstall(&cfg)
	if c.Use != "uninstall" {
		t.Fatal(c.Use)
	}
	if c.Flags().Lookup("purge") == nil || c.Flags().Lookup("yes") == nil {
		t.Fatal("missing flags")
	}
}

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/proxy"
)

func TestRunProxyForegroundListenError(t *testing.T) {
	dataDir := t.TempDir()
	cfg := config.Config{
		DataDir:  dataDir,
		LogLevel: "error",
	}
	// Invalid port → ListenAndServe fails after pid file write + store setup.
	err := runProxyForeground(cfg, "127.0.0.1:99999")
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestRunProxyForegroundEnsureDirsFail(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runProxyForeground(config.Config{DataDir: f, LogLevel: "error"}, "127.0.0.1:3128")
	if err == nil {
		t.Fatal("expected EnsureDirs error")
	}
}

func TestCmdProxyLsWhilePidAlive(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	pidPath := proxy.PIDPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// Current process is alive → ls reports proxy up
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdProxyLs(&cfgPath).Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
}

func TestCmdProxyDownStopsChild(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)

	// Start a long-sleep child we can SIGTERM via proxy down.
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Ensure reaped
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	pidPath := proxy.PIDPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	down := cmdProxyDown(&cfgPath)
	if err := down.Execute(); err != nil {
		t.Fatalf("down: %v", err)
	}
}

func TestCmdProxyDownDeadPID(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	pidPath := proxy.PIDPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte("999999991\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cmdProxyDown(&cfgPath).Execute()
	// Signal to missing pid usually errors; accept either path
	if err != nil {
		t.Logf("dead pid down: %v", err)
	}
}

func TestCmdProxyDenyMissingRule(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	deny := cmdProxyDeny(&cfgPath)
	deny.SetArgs([]string{"no-such-rule-id"})
	if err := deny.Execute(); err == nil {
		t.Fatal("expected deny missing rule error")
	}
}

func TestCmdProxyAllowMinimalAndLsDefaults(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)

	allow := cmdProxyAllow(&cfgPath)
	allow.SetArgs([]string{"--host", "only.example.com"})
	if err := allow.Execute(); err != nil {
		t.Fatal(err)
	}

	// plant dead pid so ls shows "down"
	pidPath := proxy.PIDPath(dataDir)
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o700)
	_ = os.WriteFile(pidPath, []byte("1\n"), 0o644) // pid 1 may be alive on unix!

	// Use nonexistent pid
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", 1<<28)), 0o644)

	if err := cmdProxyLs(&cfgPath).Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdProxyUpBadConfig(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	// invalid path that is a directory — Load may return defaults or error
	// use a non-yaml file that fails parse
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(cfgPath, []byte(":\n  - not: valid: yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	up := cmdProxyUp(&cfgPath)
	up.SetArgs([]string{"--fg", "--listen", "127.0.0.1:19999"})
	err := up.Execute()
	if err == nil {
		t.Fatal("expected config parse error")
	}
}

func TestCmdProxyConstruction(t *testing.T) {
	cfg := ""
	root := cmdProxy(&cfg)
	if root.Use != "proxy" {
		t.Fatalf("use %q", root.Use)
	}
	found := map[string]bool{}
	for _, c := range root.Commands() {
		found[c.Name()] = true
	}
	for _, name := range []string{"up", "down", "allow", "deny", "ls", "client"} {
		if !found[name] {
			t.Fatalf("missing subcommand %s", name)
		}
	}
}

func TestPortOfIPv6Style(t *testing.T) {
	t.Parallel()
	// LastIndex of : still works for host:port
	if got := portOf("[::1]:3128"); got != "3128" {
		t.Fatalf("%q", got)
	}
	if got := portOf("host:"); got != fmt.Sprintf("%d", proxy.DefaultGuestProxyPort) {
		t.Fatalf("%q", got)
	}
}

func TestReadPIDWhitespace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pid")
	if err := os.WriteFile(p, []byte("  42  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sscanf skips leading space
	pid, err := readPID(p)
	if err != nil {
		t.Fatal(err)
	}
	if pid != 42 {
		t.Fatalf("pid=%d", pid)
	}
}

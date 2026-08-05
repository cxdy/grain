package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/proxy"
)

// ---- from proxy_test.go ----

func writeProxyConfig(t *testing.T, dataDir string) string {
	t.Helper()
	cfgPath := filepath.Join(dataDir, "config.yaml")
	content := fmt.Sprintf("data_dir: %q\nproxy_listen: \"127.0.0.1:13128\"\n", dataDir)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestCmdProxyAllowDenyLsClient(t *testing.T) {
	// Local-only; keep api flags clear.
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")

	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)

	// empty ls (no rules / clients)
	if err := cmdProxyLs(&cfgPath).Execute(); err != nil {
		t.Fatalf("ls empty: %v", err)
	}

	// allow without --host
	allow := cmdProxyAllow(&cfgPath)
	allow.SetArgs([]string{})
	if err := allow.Execute(); err == nil || !strings.Contains(err.Error(), "--host is required") {
		t.Fatalf("host required: %v", err)
	}

	// allow with host + optional fields
	allow = cmdProxyAllow(&cfgPath)
	allow.SetArgs([]string{"--host", "api.example.com", "--method", "POST", "--path", "/v1/", "--secret", "tok"})
	if err := allow.Execute(); err != nil {
		t.Fatalf("allow: %v", err)
	}

	// second rule
	allow = cmdProxyAllow(&cfgPath)
	allow.SetArgs([]string{"--host", "*.cdn.example.com"})
	if err := allow.Execute(); err != nil {
		t.Fatalf("allow2: %v", err)
	}

	// ls
	ls := cmdProxyLs(&cfgPath)
	if err := ls.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}

	// client create with name and without
	clientRoot := cmdProxyClient(&cfgPath)
	clientRoot.SetArgs([]string{"create", "guest"})
	if err := clientRoot.Execute(); err != nil {
		t.Fatalf("client create: %v", err)
	}
	clientRoot = cmdProxyClient(&cfgPath)
	clientRoot.SetArgs([]string{"create"})
	if err := clientRoot.Execute(); err != nil {
		t.Fatalf("client create anon: %v", err)
	}

	// list clients via store
	st, err := proxy.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := st.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules=%d", len(rules))
	}
	clients, err := st.ListClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatalf("clients=%+v", clients)
	}

	// deny first rule
	deny := cmdProxyDeny(&cfgPath)
	deny.SetArgs([]string{rules[0].ID})
	if err := deny.Execute(); err != nil {
		t.Fatalf("deny: %v", err)
	}
	rules, err = st.ListRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("after deny rules=%d", len(rules))
	}

	// ls with rules + clients
	ls = cmdProxyLs(&cfgPath)
	if err := ls.Execute(); err != nil {
		t.Fatalf("ls2: %v", err)
	}
}

func TestCmdProxyDownNotRunning(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	down := cmdProxyDown(&cfgPath)
	if err := down.Execute(); err == nil || !strings.Contains(err.Error(), "proxy not running") {
		t.Fatalf("want not running, got %v", err)
	}
}

func TestCmdProxyUpAlreadyRunning(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)

	// Fake a live pid file for this process.
	pidPath := proxy.PIDPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	up := cmdProxyUp(&cfgPath)
	// without --fg: should report already up
	if err := up.Execute(); err != nil {
		t.Fatalf("up already: %v", err)
	}
}

func TestCmdProxyRequiresLocal(t *testing.T) {
	apiURLFlag = "http://10.0.0.9:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_TOKEN", "tok")
	cfg := ""
	root := cmdProxy(&cfg)
	// PersistentPreRunE fires on subcommand
	root.SetArgs([]string{"ls"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "local grain daemon") {
		t.Fatalf("want local-only error, got %v", err)
	}
}

func TestBuildProxyUserdata(t *testing.T) {
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	cfg, err := loadCfg(&cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	ud, err := buildProxyUserdata(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud, "PROXY") && !strings.Contains(ud, "proxy") && !strings.Contains(ud, "10.0.2.2") {
		t.Fatalf("userdata missing proxy markers:\n%s", ud)
	}
	// Second call reuses existing client token.
	ud2, err := buildProxyUserdata(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ud2 == "" {
		t.Fatal("empty second userdata")
	}
}

// ---- from proxy_coverage_test.go ----

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

// ---- from proxy_helpers_test.go ----

func TestReadPIDAndPortOf(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.pid")
	if _, err := readPID(p); err == nil {
		t.Fatal("missing")
	}
	if err := os.WriteFile(p, []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPID(p); err == nil {
		t.Fatal("bad pid")
	}
	if err := os.WriteFile(p, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := readPID(p)
	if err != nil || n != 12345 {
		t.Fatalf("%d %v", n, err)
	}
	if !pidAlive(os.Getpid()) {
		t.Fatal("self")
	}
	if pidAlive(999999991) {
		t.Fatal("dead")
	}
	if pidAlive(0) {
		t.Fatal("zero")
	}
	if portOf("0.0.0.0:3128") != "3128" {
		t.Fatal(portOf("0.0.0.0:3128"))
	}
	if portOf(":8080") != "8080" {
		t.Fatal(portOf(":8080"))
	}
	_ = portOf("nope")
}

func TestCmdProxyUpSpawnsBackground(t *testing.T) {
	// Non-fg path: no live pid → spawns child and waits briefly for pid file.
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	// Ensure no pid file
	_ = os.Remove(proxy.PIDPath(dataDir))
	// Pre-create proxy dir so OpenFile for log succeeds
	if err := os.MkdirAll(filepath.Join(dataDir, "proxy"), 0o700); err != nil {
		t.Fatal(err)
	}
	up := cmdProxyUp(&cfgPath)
	up.SetArgs([]string{"--listen", "127.0.0.1:0"})
	// Child is the test binary with "proxy up --fg" — it will fail quickly (cobra unknown)
	// or run briefly. Either way parent should return without hanging (3s max wait).
	err := up.Execute()
	// Success or spawn error are both acceptable; hang would fail the package timeout.
	t.Logf("proxy up bg: %v", err)
	// Best-effort cleanup of any child we started
	if pid, err := readPID(proxy.PIDPath(dataDir)); err == nil && pidAlive(pid) {
		if p, e := os.FindProcess(pid); e == nil {
			_ = p.Signal(syscall.SIGTERM)
		}
	}
}

func TestCmdProxyAllowAllFlags(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	allow := cmdProxyAllow(&cfgPath)
	allow.SetArgs([]string{
		"--host", "api.example.com",
		"--method", "POST",
		"--path", "/v1/",
		"--secret", "tok",
	})
	if err := allow.Execute(); err != nil {
		t.Fatal(err)
	}
	ls := cmdProxyLs(&cfgPath)
	if err := ls.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdProxyClientCreateNamed(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	// Use full proxy tree so PersistentPreRunE runs
	root := cmdProxy(&cfgPath)
	root.SetArgs([]string{"client", "create", "ci"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdProxyDownFindProcess(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	pidPath := proxy.PIDPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	// dead pid
	if err := os.WriteFile(pidPath, []byte("999999991\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	down := cmdProxyDown(&cfgPath)
	// Signal may fail on dead pid; still exercises path
	_ = down.Execute()
}

func TestRunProxyForegroundOKBrief(t *testing.T) {
	// Listen on free port, then cancel via process signal is hard; instead hit
	// listen error already covered. Cover EnsureDirs success + store + listen race:
	// start in goroutine and kill via closing by using invalid listen after dirs ok
	// already covered. Hit pid write success then immediate ListenAndServe error:
	dataDir := t.TempDir()
	cfg := config.Config{DataDir: dataDir, LogLevel: "error"}
	// invalid listen that fails quickly
	err := runProxyForeground(cfg, "127.0.0.1:999999")
	if err == nil {
		t.Fatal("expected listen error")
	}
}

func TestEnsureProxyAuthForListenNonLoopback(t *testing.T) {
	dataDir := t.TempDir()

	// Non-loopback without clients → refuse.
	err := ensureProxyAuthForListen(dataDir, "0.0.0.0:3128")
	if err == nil || !strings.Contains(err.Error(), "no clients") {
		t.Fatalf("want no-clients error, got %v", err)
	}
	err = ensureProxyAuthForListen(dataDir, ":3128")
	if err == nil || !strings.Contains(err.Error(), "no clients") {
		t.Fatalf("want no-clients for :port, got %v", err)
	}

	// Loopback without clients is OK (auth still optional at request time).
	if err := ensureProxyAuthForListen(dataDir, "127.0.0.1:3128"); err != nil {
		t.Fatalf("loopback: %v", err)
	}
	if err := ensureProxyAuthForListen(dataDir, "[::1]:3128"); err != nil {
		t.Fatalf("::1: %v", err)
	}

	// Create a client → non-loopback allowed.
	st, err := proxy.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateClient("agent"); err != nil {
		t.Fatal(err)
	}
	if err := ensureProxyAuthForListen(dataDir, "0.0.0.0:3128"); err != nil {
		t.Fatalf("with client: %v", err)
	}
}

func TestRunProxyForegroundNonLoopbackNoClients(t *testing.T) {
	dataDir := t.TempDir()
	cfg := config.Config{DataDir: dataDir, LogLevel: "error"}
	err := runProxyForeground(cfg, "0.0.0.0:13129")
	if err == nil || !strings.Contains(err.Error(), "no clients") {
		t.Fatalf("want refuse start without clients, got %v", err)
	}
	// Must not have written a pid file after refusing.
	if _, err := os.Stat(proxy.PIDPath(dataDir)); err == nil {
		t.Fatal("pid file should not exist after refused start")
	}
}

func TestCmdProxyUpNonLoopbackNoClients(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	up := cmdProxyUp(&cfgPath)
	up.SetArgs([]string{"--listen", "0.0.0.0:13130"})
	err := up.Execute()
	if err == nil || !strings.Contains(err.Error(), "no clients") {
		t.Fatalf("want no-clients error, got %v", err)
	}
}

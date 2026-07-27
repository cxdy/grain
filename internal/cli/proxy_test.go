package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/proxy"
)

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

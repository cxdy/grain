package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
)

func TestAPIURLAndListenLoopback(t *testing.T) {
	t.Parallel()
	if !config.APIURLIsLoopback("") {
		t.Fatal("empty")
	}
	if !config.APIURLIsLoopback("http://127.0.0.1:7474") {
		t.Fatal("127")
	}
	if !config.APIURLIsLoopback("http://localhost:1") {
		t.Fatal("localhost")
	}
	if !config.APIURLIsLoopback("http://grain/") {
		t.Fatal("grain")
	}
	if config.APIURLIsLoopback("http://example.com:7474") {
		t.Fatal("remote")
	}
	if config.APIURLIsLoopback("://bad") {
		t.Fatal("bad")
	}

	if !config.ListenAddrIsLoopback("") {
		t.Fatal("empty listen")
	}
	if !config.ListenAddrIsLoopback("127.0.0.1:7474") {
		t.Fatal("127 listen")
	}
	if !config.ListenAddrIsLoopback("localhost:9") {
		t.Fatal("localhost listen")
	}
	if config.ListenAddrIsLoopback(":7474") {
		t.Fatal("all ifaces")
	}
	if config.ListenAddrIsLoopback("0.0.0.0:7474") {
		t.Fatal("0.0.0.0")
	}

	n := config.NormalizeAPIURL("127.0.0.1:7474")
	if n == "" || n[:4] != "http" {
		t.Fatal(n)
	}
	if config.NormalizeAPIURL("http://x") != "http://x" {
		t.Fatal()
	}
}

func TestLookupProfileAndResolveCreate(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Profiles = map[string]config.Profile{
		"dev": {
			CPUs: 4, MemoryMB: 4096, DiskGB: 20, Image: "ubuntu-cloud",
			Mounts:   []config.ProfileMount{{Host: "/tmp", Guest: "/work"}},
			Forwards: []config.ProfileForward{{HostPort: 0, GuestPort: 8080}},
		},
	}
	if _, err := cfg.LookupProfile(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := cfg.LookupProfile("nope"); err == nil {
		t.Fatal("missing")
	}
	p, err := cfg.LookupProfile("dev")
	if err != nil || p.CPUs != 4 {
		t.Fatal(err, p)
	}
	names := cfg.ProfileNames()
	if len(names) != 1 || names[0] != "dev" {
		t.Fatal(names)
	}

	r, err := cfg.ResolveCreate("dev", config.CreateOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if r.CPUs != 4 || r.Image != "ubuntu-cloud" {
		t.Fatalf("%+v", r)
	}
	// flag overrides
	r, err = cfg.ResolveCreate("dev", config.CreateOverrides{
		CPUs: 1, CPUsSet: true, MemoryMB: 512, MemoryMBSet: true,
	})
	if err != nil || r.CPUs != 1 || r.MemoryMB != 512 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestLoadAndEnsureDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "data_dir: " + dir + "/d\nready_timeout: 30s\nhypervisor: mock\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Hypervisor != "mock" {
		t.Fatal(cfg.Hypervisor)
	}
	if cfg.ReadyTimeout != 30*time.Second {
		t.Fatal(cfg.ReadyTimeout)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.DataDir); err != nil {
		t.Fatal(err)
	}
	// missing file → defaults
	cfg2, err := config.Load(filepath.Join(dir, "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.DefaultCPUs == 0 {
		t.Fatal("defaults")
	}
}

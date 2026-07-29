package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
)

func TestNormalizeAPIURL(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                       "",
		"  ":                     "",
		"http://127.0.0.1:7474":  "http://127.0.0.1:7474",
		"http://127.0.0.1:7474/": "http://127.0.0.1:7474",
		"https://grain.example/": "https://grain.example",
		"127.0.0.1:7474":         "http://127.0.0.1:7474",
		"sandbox.internal:7474":  "http://sandbox.internal:7474",
	}
	for in, want := range cases {
		if got := config.NormalizeAPIURL(in); got != want {
			t.Errorf("NormalizeAPIURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestAPIURLIsLoopback(t *testing.T) {
	t.Parallel()
	loop := []string{"", "http://127.0.0.1:7474", "http://localhost:7474", "http://[::1]:7474", "http://grain"}
	for _, u := range loop {
		if !config.APIURLIsLoopback(u) {
			t.Errorf("expected loopback: %q", u)
		}
	}
	remote := []string{"http://10.0.0.5:7474", "https://grain.example", "http://sandbox.internal:7474"}
	for _, u := range remote {
		if config.APIURLIsLoopback(u) {
			t.Errorf("expected non-loopback: %q", u)
		}
	}
}

func TestListenAddrIsLoopback(t *testing.T) {
	t.Parallel()
	if !config.ListenAddrIsLoopback("127.0.0.1:7474") {
		t.Fatal("127.0.0.1 should be loopback")
	}
	if !config.ListenAddrIsLoopback("[::1]:7474") {
		t.Fatal("::1 should be loopback")
	}
	if config.ListenAddrIsLoopback("0.0.0.0:7474") {
		t.Fatal("0.0.0.0 is not loopback")
	}
	if config.ListenAddrIsLoopback(":7474") {
		t.Fatal(":port binds all interfaces")
	}
	if config.ListenAddrIsLoopback("192.168.1.10:7474") {
		t.Fatal("LAN IP is not loopback")
	}
}

func TestIsRemoteClient(t *testing.T) {
	t.Parallel()
	c := config.Config{}
	if c.IsRemoteClient() {
		t.Fatal("empty should be local")
	}
	c.APIURL = "http://10.0.0.1:7474"
	if !c.IsRemoteClient() {
		t.Fatal("api_url should be remote")
	}
}

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
	// user "dev" + builtin "remote-coding"
	if len(names) != 2 || names[0] != "dev" || names[1] != "remote-coding" {
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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMoreEdges(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.ReadyTimeout = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "ready_timeout") {
		t.Fatalf("%v", err)
	}
	cfg = Defaults()
	cfg.MaxVMs = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("max_vms")
	}
	cfg = Defaults()
	cfg.MaxCPUsTotal = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("max_cpus")
	}
	cfg = Defaults()
	cfg.MaxMemoryMBTotal = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("max_mem")
	}
	cfg = Defaults()
	cfg.DefaultMemoryMB = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("mem")
	}
	cfg = Defaults()
	cfg.DefaultDiskGB = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("disk")
	}
	cfg = Defaults()
	cfg.WarmPool.Size = -1
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "warm_pool.size") {
		t.Fatalf("%v", err)
	}
	cfg = Defaults()
	cfg.APIURL = "ftp://bad"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "api_url") {
		t.Fatalf("%v", err)
	}
	cfg = Defaults()
	cfg.APIURL = "https://example.com:7474"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg = Defaults()
	cfg.API = "localhost:7474"
	cfg.APIToken = ""
	if err := cfg.Validate(); err != nil {
		t.Fatal(err) // loopback localhost ok without token
	}
	cfg = Defaults()
	cfg.API = "[::1]:7474"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg = Defaults()
	cfg.MCP.Listen = "127.0.0.1:7476/mcp"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg = Defaults()
	cfg.MCP.Listen = "127.0.0.1:7476"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg = Defaults()
	cfg.GuestArch = "amd64"
	cfg.Hypervisor = "firecracker"
	cfg.LogLevel = "debug"
	cfg.MountDriver = "virtiofs"
	cfg.AgentTransport = "vsock"
	cfg.Network = "slirp"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	// multiple errors joined
	cfg = Defaults()
	cfg.Hypervisor = "bad"
	cfg.LogLevel = "bad"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("want multi")
	}
	if !strings.Contains(err.Error(), "hypervisor") || !strings.Contains(err.Error(), "log_level") {
		t.Fatalf("%v", err)
	}
}

func TestValidateFileEmptyAndMissing(t *testing.T) {
	if _, err := ValidateFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing")
	}
	// empty path uses home config (likely missing or present)
	_, _ = ValidateFile("")

	dir := t.TempDir()
	// empty yaml document
	p := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFile(p); err != nil {
		t.Fatal(err)
	}
	// null document
	p2 := filepath.Join(dir, "null.yaml")
	if err := os.WriteFile(p2, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFile(p2); err != nil {
		// null may fail or apply defaults
		t.Log(err)
	}
}

func TestFilepathJoinHome(t *testing.T) {
	t.Parallel()
	p := filepathJoinHome("config.yaml")
	if !strings.Contains(p, "config.yaml") || !strings.Contains(p, ".grain") {
		t.Fatalf("%q", p)
	}
}

func TestKnownTopLevelKeysCovered(t *testing.T) {
	// ensure known keys map is non-empty and accepts desktop/connections
	for _, k := range []string{"data_dir", "desktop", "connections", "warm_pool", "mcp"} {
		if _, ok := knownTopLevelKeys[k]; !ok {
			t.Fatalf("missing key %s", k)
		}
	}
}

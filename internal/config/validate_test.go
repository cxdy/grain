package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateOK(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateBadHypervisor(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Hypervisor = "xen"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "hypervisor") {
		t.Fatalf("%v", err)
	}
}

func TestValidateNonLoopbackNeedsToken(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.API = "0.0.0.0:7474"
	cfg.APIToken = ""
	cfg.AuthToken = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "api_token") {
		t.Fatalf("%v", err)
	}
	cfg.APIToken = "secret"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateEnums(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.LogLevel = "trace"
	if err := cfg.Validate(); err == nil {
		t.Fatal("log_level")
	}
	cfg = Defaults()
	cfg.MountDriver = "nfs"
	if err := cfg.Validate(); err == nil {
		t.Fatal("mount")
	}
	cfg = Defaults()
	cfg.AgentTransport = "udp"
	if err := cfg.Validate(); err == nil {
		t.Fatal("agent")
	}
	cfg = Defaults()
	cfg.GuestArch = "riscv"
	if err := cfg.Validate(); err == nil {
		t.Fatal("arch")
	}
	cfg = Defaults()
	cfg.GPU = "metal"
	if err := cfg.Validate(); err == nil {
		t.Fatal("gpu")
	}
	cfg = Defaults()
	cfg.Network = "bridge"
	if err := cfg.Validate(); err == nil {
		t.Fatal("network")
	}
	cfg = Defaults()
	cfg.DefaultCPUs = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("cpus")
	}
	cfg = Defaults()
	cfg.API = "not-a-port"
	if err := cfg.Validate(); err == nil {
		t.Fatal("api")
	}
	cfg = Defaults()
	cfg.API = "10.0.0.5:7474"
	cfg.APIToken = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("token required")
	}
	cfg = Defaults()
	cfg.MCP.Listen = "noport"
	if err := cfg.Validate(); err == nil {
		t.Fatal("mcp")
	}
	cfg = Defaults()
	cfg.GuestArch = "arm64"
	cfg.GPU = "virtio"
	cfg.Network = "overlay"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateWarmPool(t *testing.T) {
	cfg := Defaults()
	cfg.WarmPool = WarmPoolConfig{Template: "golden", Size: 2}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.WarmPool.Size = 33
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "warm_pool.size") {
		t.Fatalf("want size error, got %v", err)
	}
	cfg.WarmPool = WarmPoolConfig{Template: "", Size: 2}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "template") {
		t.Fatalf("want template error, got %v", err)
	}
}

func TestValidateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: qemu\ncpus: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFile(path); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("hypervisor: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFile(bad); err == nil {
		t.Fatal("want error")
	}
	// invalid yaml
	yuck := filepath.Join(dir, "y.yaml")
	if err := os.WriteFile(yuck, []byte(":\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFile(yuck); err == nil {
		t.Fatal("want parse error")
	}
	// unknown top-level keys rejected
	unk := filepath.Join(dir, "u.yaml")
	if err := os.WriteFile(unk, []byte("urmom:\n  is: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateFile(unk); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("want unknown key error, got %v", err)
	}
}

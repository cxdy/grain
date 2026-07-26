package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
)

func TestDefaults(t *testing.T) {
	t.Parallel()
	c := config.Defaults()
	if c.DefaultCPUs < 1 {
		t.Fatal("cpus")
	}
	if c.Socket == "" || c.DataDir == "" {
		t.Fatal("paths")
	}
	if c.Hypervisor != "qemu" {
		t.Fatalf("hypervisor %s", c.Hypervisor)
	}
	if c.AgentTransport != "auto" {
		t.Fatalf("agent_transport %s", c.AgentTransport)
	}
	// finite resource caps so hosts cannot thrash by default
	if c.MaxVMs != 8 {
		t.Fatalf("max_vms %d", c.MaxVMs)
	}
	if c.MaxCPUsTotal != 16 {
		t.Fatalf("max_cpus_total %d", c.MaxCPUsTotal)
	}
	if c.MaxMemoryMBTotal != 32768 {
		t.Fatalf("max_memory_mb_total %d", c.MaxMemoryMBTotal)
	}
	if c.MaxCPUsPerVM != 8 {
		t.Fatalf("max_cpus_per_vm %d", c.MaxCPUsPerVM)
	}
	if c.MaxMemoryMBPerVM != 16384 {
		t.Fatalf("max_memory_mb_per_vm %d", c.MaxMemoryMBPerVM)
	}
}

func TestLoadMissingUsesDefaults(t *testing.T) {
	t.Parallel()
	c, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultMemoryMB != 2048 {
		t.Fatalf("memory %d", c.DefaultMemoryMB)
	}
}

func TestLoadYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("cpus: 4\nmemory_mb: 4096\nhypervisor: mock\nmax_vms: 2\nmax_cpus_per_vm: 4\nagent_transport: vsock\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultCPUs != 4 || c.DefaultMemoryMB != 4096 {
		t.Fatalf("got cpus=%d mem=%d", c.DefaultCPUs, c.DefaultMemoryMB)
	}
	if c.Hypervisor != "mock" {
		t.Fatalf("hypervisor %s", c.Hypervisor)
	}
	if c.AgentTransport != "vsock" {
		t.Fatalf("agent_transport %s", c.AgentTransport)
	}
	// zero duration filled in
	if c.ReadyTimeout < time.Second {
		t.Fatalf("ready timeout %v", c.ReadyTimeout)
	}
	if c.MaxVMs != 2 || c.MaxCPUsPerVM != 4 {
		t.Fatalf("caps max_vms=%d max_cpus_per_vm=%d", c.MaxVMs, c.MaxCPUsPerVM)
	}
	// unset caps keep defaults
	if c.MaxMemoryMBPerVM != 16384 {
		t.Fatalf("max_memory_mb_per_vm default %d", c.MaxMemoryMBPerVM)
	}
}

func TestLoadYAMLZeroCapMeansUnlimited(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// explicit 0 overrides default → unlimited for that field
	body := []byte("max_vms: 0\nmax_cpus_total: 0\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxVMs != 0 || c.MaxCPUsTotal != 0 {
		t.Fatalf("want zero (unlimited), got max_vms=%d max_cpus_total=%d", c.MaxVMs, c.MaxCPUsTotal)
	}
}

func TestLoadFirecrackerFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("hypervisor: firecracker\nfirecracker_binary: /usr/bin/firecracker\nkernel_path: /opt/vmlinux\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hypervisor != "firecracker" {
		t.Fatalf("hypervisor %s", c.Hypervisor)
	}
	if c.FirecrackerBinary != "/usr/bin/firecracker" {
		t.Fatalf("firecracker_binary %s", c.FirecrackerBinary)
	}
	if c.KernelPath != "/opt/vmlinux" {
		t.Fatalf("kernel_path %s", c.KernelPath)
	}
}

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
	body := []byte("cpus: 4\nmemory_mb: 4096\nhypervisor: mock\n")
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
	// zero duration filled in
	if c.ReadyTimeout < time.Second {
		t.Fatalf("ready timeout %v", c.ReadyTimeout)
	}
}

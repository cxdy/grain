package manager

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func TestSpawnFromSuspendedTemplate(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = 2 * time.Second
	st, err := store.New(filepath.Join(dir, "vms"))
	if err != nil {
		t.Fatal(err)
	}
	rt := hypervisor.NewMockRuntime()
	disk := hypervisor.NewMockDisk()
	m := New(cfg, st, rt, disk, nil)

	ctx := context.Background()
	tpl, err := m.Create(ctx, vm.CreateOpts{
		Name: "golden", Persistent: true, Image: "ubuntu-cloud", CPUs: 1, MemoryMB: 512, WaitMode: vm.WaitSSH,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(ctx, tpl.Name); err != nil {
		t.Fatal(err)
	}
	if tag, ok := readSuspendMarker(st.Dir(tpl.Name)); !ok || tag == "" {
		t.Fatalf("expected suspend marker, ok=%v tag=%q", ok, tag)
	}

	t0 := time.Now()
	child, err := m.Spawn(ctx, "golden", "fast-1")
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	if child.Name != "fast-1" || child.Status != vm.StatusRunning {
		t.Fatalf("%+v", child)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("spawn too slow on mock: %s", elapsed)
	}
	src, err := m.Get("golden")
	if err != nil || src.Status != vm.StatusSuspended {
		t.Fatalf("template: %+v %v", src, err)
	}
}

func TestSpawnRequiresStoppedTemplate(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	st, err := store.New(filepath.Join(dir, "vms"))
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	ctx := context.Background()
	if _, err := m.Create(ctx, vm.CreateOpts{Name: "run", Persistent: true, Image: "ubuntu-cloud", WaitMode: vm.WaitSSH}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Spawn(ctx, "run", "x"); err == nil {
		t.Fatal("expected error spawning from running template")
	}
}

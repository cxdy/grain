package manager

import (
	"context"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func testPoolManager(t *testing.T, size int) *Manager {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = 2 * time.Second
	cfg.WarmPool = config.WarmPoolConfig{Template: "golden", Size: size}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	t.Cleanup(func() { m.WaitPoolBackground() })
	return m
}

func TestPoolFillClaimDrain(t *testing.T) {
	m := testPoolManager(t, 2)
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

	st, err := m.PoolFill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || st.Ready != 2 || st.Desired != 2 {
		t.Fatalf("status after fill: %+v", st)
	}
	if len(st.Members) != 2 {
		t.Fatalf("members %v", st.Members)
	}

	// Template still suspended.
	src, err := m.Get("golden")
	if err != nil || src.Status != vm.StatusSuspended {
		t.Fatalf("template: %+v %v", src, err)
	}

	t0 := time.Now()
	child, err := m.PoolClaim(ctx, "work-1")
	elapsed := time.Since(t0)
	if err != nil {
		t.Fatal(err)
	}
	if child.Name != "work-1" || child.Status != vm.StatusRunning {
		t.Fatalf("%+v", child)
	}
	if child.Tags != nil {
		if _, ok := child.Tags[tagPool]; ok {
			t.Fatalf("pool tag should be cleared: %v", child.Tags)
		}
	}
	if elapsed > 2*time.Second {
		t.Fatalf("claim too slow on mock: %s", elapsed)
	}

	// Wait for async refill to restore size=2.
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err = m.PoolStatus()
		if err != nil {
			t.Fatal(err)
		}
		if st.Ready >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refill did not restore ready=2: %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}

	n, err := m.PoolDrain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("drained %d", n)
	}
	st, err = m.PoolStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.Ready != 0 {
		t.Fatalf("after drain: %+v", st)
	}
	// Claimed VM still exists.
	if _, err := m.Get("work-1"); err != nil {
		t.Fatal(err)
	}
}

func TestPoolClaimEmptyWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	// No warm_pool config
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if _, err := m.PoolClaim(context.Background(), "x"); err == nil {
		t.Fatal("expected empty pool error")
	}
}

func TestPoolClaimAutoFill(t *testing.T) {
	m := testPoolManager(t, 1)
	ctx := context.Background()
	if _, err := m.Create(ctx, vm.CreateOpts{
		Name: "golden", Persistent: true, Image: "ubuntu-cloud", WaitMode: vm.WaitSSH,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(ctx, "golden"); err != nil {
		t.Fatal(err)
	}
	// Claim without prior fill — fill on demand.
	inst, err := m.PoolClaim(ctx, "auto-1")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "auto-1" || inst.Status != vm.StatusRunning {
		t.Fatalf("%+v", inst)
	}
}

func TestWarmPoolConfigEnabled(t *testing.T) {
	t.Parallel()
	if (config.WarmPoolConfig{}).Enabled() {
		t.Fatal("empty should be disabled")
	}
	if (config.WarmPoolConfig{Template: "g", Size: 0}).Enabled() {
		t.Fatal("size 0 disabled")
	}
	if !(config.WarmPoolConfig{Template: "g", Size: 1}).Enabled() {
		t.Fatal("should enable")
	}
}

func TestRunningWarmPoolFillClaim(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = 2 * time.Second
	cfg.WarmPool = config.WarmPoolConfig{Template: "golden", Size: 1, Running: true}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	t.Cleanup(func() { m.WaitPoolBackground() })
	ctx := context.Background()

	if _, err := m.Create(ctx, vm.CreateOpts{
		Name: "golden", Persistent: true, Image: "ubuntu-cloud", CPUs: 1, MemoryMB: 512, WaitMode: vm.WaitSSH,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(ctx, "golden"); err != nil {
		t.Fatal(err)
	}

	pst, err := m.PoolFill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !pst.Running || pst.Ready != 1 {
		t.Fatalf("status: %+v", pst)
	}
	// Member should be running in running mode.
	if len(pst.Members) != 1 {
		t.Fatalf("members: %v", pst.Members)
	}
	mem, err := m.Get(pst.Members[0])
	if err != nil {
		t.Fatal(err)
	}
	if mem.Status != vm.StatusRunning {
		t.Fatalf("want running pool member, got %s", mem.Status)
	}

	child, err := m.PoolClaim(ctx, "work-run")
	if err != nil {
		t.Fatal(err)
	}
	if child.Name != "work-run" || child.Status != vm.StatusRunning {
		t.Fatalf("%+v", child)
	}
	if child.Tags != nil {
		if _, ok := child.Tags[tagPool]; ok {
			t.Fatalf("pool tag should be cleared: %v", child.Tags)
		}
	}
}

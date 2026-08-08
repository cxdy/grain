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

func TestPoolFillDisabledAndMissingTemplate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	// Disabled warm pool
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	ctx := context.Background()

	pst, err := m.PoolFill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pst.Enabled || pst.Ready != 0 {
		t.Fatalf("disabled fill: %+v", pst)
	}
	if err := m.EnsureWarmPool(ctx); err != nil {
		t.Fatal(err)
	}

	// Enabled but template missing.
	m.cfg.WarmPool = config.WarmPoolConfig{Template: "no-such", Size: 1}
	if _, err := m.PoolFill(ctx); err == nil {
		t.Fatal("expected missing template error")
	}
	if err := m.EnsureWarmPool(ctx); err == nil {
		t.Fatal("expected ensure fail")
	}
}

func TestPoolFillContextCancel(t *testing.T) {
	m := testPoolManager(t, 2)
	ctx := context.Background()
	if _, err := m.Create(ctx, vm.CreateOpts{
		Name: "golden", Persistent: true, Image: "ubuntu-cloud", WaitMode: vm.WaitSSH,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(ctx, "golden"); err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.PoolFill(cctx); err == nil {
		t.Fatal("expected context cancel")
	}
}

func TestPoolClaimDestEqualsMemberAndConflict(t *testing.T) {
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
	pst, err := m.PoolFill(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pst.Members) != 1 {
		t.Fatalf("members %v", pst.Members)
	}
	member := pst.Members[0]

	// Dest name already taken → claimCreateName error.
	if _, err := m.Create(ctx, vm.CreateOpts{
		Name: "taken", Persistent: true, Image: "ubuntu-cloud", WaitMode: vm.WaitSSH,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(ctx, "taken"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PoolClaim(ctx, "taken"); err == nil {
		t.Fatal("expected name conflict")
	}

	// Claim with a free dest name (rename from pool member).
	// dest == member is not supported once the member already occupies that name.
	pst, err = m.PoolStatus()
	if err != nil {
		t.Fatal(err)
	}
	if len(pst.Members) != 1 || pst.Members[0] != member {
		t.Fatalf("member after conflict: %v", pst.Members)
	}
	inst, err := m.PoolClaim(ctx, "work-from-pool")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "work-from-pool" {
		t.Fatalf("name %s", inst.Name)
	}
	if inst.Tags != nil {
		if _, ok := inst.Tags[tagPool]; ok {
			t.Fatalf("pool tag should be cleared: %v", inst.Tags)
		}
	}
}

func TestPoolFillRunningStartFail(t *testing.T) {
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
	rt := hypervisor.NewMockRuntime()
	m := New(cfg, st, rt, hypervisor.NewMockDisk(), nil)
	t.Cleanup(func() { m.WaitPoolBackground() })
	ctx := context.Background()

	if _, err := m.Create(ctx, vm.CreateOpts{
		Name: "golden", Persistent: true, Image: "ubuntu-cloud", WaitMode: vm.WaitSSH,
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(ctx, "golden"); err != nil {
		t.Fatal(err)
	}
	rt.FailStart = true
	if _, err := m.PoolFill(ctx); err == nil {
		t.Fatal("expected running-mode start failure")
	}
}

func TestListPoolMembersFiltersAndHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.WarmPool = config.WarmPoolConfig{Template: "tpl", Size: 2}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rt := hypervisor.NewMockRuntime()
	m := New(cfg, st, rt, hypervisor.NewMockDisk(), nil)

	now := time.Now().UTC()
	// Nil tags / wrong tag / wrong template
	_ = st.Put(&vm.Instance{Name: "a", Status: vm.StatusStopped})
	_ = st.Put(&vm.Instance{Name: "b", Status: vm.StatusStopped, Tags: map[string]string{"x": "1"}})
	_ = st.Put(&vm.Instance{
		Name: "c", Status: vm.StatusStopped, CreatedAt: now.Add(-time.Hour),
		Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "other"},
	})
	// Creating skipped
	_ = st.Put(&vm.Instance{
		Name: "d", Status: vm.StatusCreating, CreatedAt: now.Add(-2 * time.Hour),
		Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "tpl"},
	})
	// Running meta status skipped in disk mode
	_ = st.Put(&vm.Instance{
		Name: "e", Status: vm.StatusRunning, CreatedAt: now.Add(-30 * time.Minute),
		Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "tpl"},
	})
	// Paused skipped in disk mode
	_ = st.Put(&vm.Instance{
		Name: "f", Status: vm.StatusPaused, CreatedAt: now.Add(-20 * time.Minute),
		Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "tpl"},
	})
	// Claimable, older first
	_ = st.Put(&vm.Instance{
		Name: "g-old", Status: vm.StatusStopped, CreatedAt: now.Add(-3 * time.Hour),
		Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "tpl"},
	})
	_ = st.Put(&vm.Instance{
		Name: "g-new", Status: vm.StatusSuspended, CreatedAt: now.Add(-10 * time.Minute),
		Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "tpl"},
	})
	// Live runtime running but status stopped → filtered
	live := &vm.Instance{
		Name: "live", Status: vm.StatusStopped, CreatedAt: now.Add(-5 * time.Minute),
		Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "tpl"},
	}
	_ = st.Put(live)
	_ = rt.Start(context.Background(), live, dir+"/disk")

	list, err := m.listPoolMembers("tpl")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "g-old" || list[1].Name != "g-new" {
		names := make([]string, len(list))
		for i, i2 := range list {
			names[i] = i2.Name
		}
		t.Fatalf("disk mode members %v", names)
	}

	// Empty template → all pool-tagged claimable (disk mode still filters running).
	all, err := m.listPoolMembers("")
	if err != nil {
		t.Fatal(err)
	}
	// g-old, g-new, and c (other template)
	if len(all) != 3 {
		t.Fatalf("all pool members %d", len(all))
	}

	// Running mode: only live/running/paused claimable.
	m.cfg.WarmPool.Running = true
	runList, err := m.listPoolMembers("tpl")
	if err != nil {
		t.Fatal(err)
	}
	// e (StatusRunning), f (paused), live (rt.Running)
	if len(runList) < 2 {
		t.Fatalf("running mode got %d", len(runList))
	}
	// Stopped without live process filtered out.
	for _, inst := range runList {
		if inst.Name == "g-old" || inst.Name == "g-new" {
			t.Fatalf("stopped member should not be claimable in running mode: %s", inst.Name)
		}
	}

	if !isPoolMember(&vm.Instance{Tags: map[string]string{tagPool: poolTagValue}}, "") {
		t.Fatal("empty template should match any pool member")
	}
	if isPoolMember(nil, "tpl") || isPoolMember(&vm.Instance{}, "tpl") {
		t.Fatal("nil/empty tags")
	}
	if poolNamePrefix("") != "pool-sbox" || poolNamePrefix("  ") != "pool-sbox" {
		t.Fatalf("prefix empty: %q %q", poolNamePrefix(""), poolNamePrefix("  "))
	}
	if poolNamePrefix("golden") != "pool-golden" {
		t.Fatalf("prefix %q", poolNamePrefix("golden"))
	}
}

func TestPoolStatusAndDrainEmptyTemplate(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = 2 * time.Second
	// No template in config → drain uses empty template filter (all pool tags).
	cfg.WarmPool = config.WarmPoolConfig{}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	ctx := context.Background()

	_ = st.Put(&vm.Instance{
		Name: "p1", Status: vm.StatusStopped, Persistent: true,
		DiskPath: dir + "/d", Tags: map[string]string{tagPool: poolTagValue, tagPoolTemplate: "t"},
	})
	// Need disk for Delete path after stop — mock delete just removes meta.
	pst, err := m.PoolStatus()
	if err != nil {
		t.Fatal(err)
	}
	if pst.Ready != 1 {
		t.Fatalf("status %+v", pst)
	}
	n, err := m.PoolDrain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("drained %d", n)
	}
}

func TestEnsureWarmPoolFills(t *testing.T) {
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
	if err := m.EnsureWarmPool(ctx); err != nil {
		t.Fatal(err)
	}
	pst, err := m.PoolStatus()
	if err != nil {
		t.Fatal(err)
	}
	if pst.Ready != 1 {
		t.Fatalf("%+v", pst)
	}
}

func TestPoolClaimAutoName(t *testing.T) {
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
	if _, err := m.PoolFill(ctx); err != nil {
		t.Fatal(err)
	}
	inst, err := m.PoolClaim(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name == "" || inst.Status != vm.StatusRunning {
		t.Fatalf("%+v", inst)
	}
}

func TestPoolPickFillFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.WarmPool = config.WarmPoolConfig{Template: "missing-tpl", Size: 1}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if _, err := m.PoolClaim(context.Background(), "x"); err == nil {
		t.Fatal("expected empty+fill failed")
	}
}

package manager_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func testManager(t *testing.T) (*manager.Manager, *hypervisor.MockRuntime, *hypervisor.MockDisk) {
	t.Helper()
	return testManagerCfg(t, config.Defaults())
}

func testManagerCfg(t *testing.T, cfg config.Config) (*manager.Manager, *hypervisor.MockRuntime, *hypervisor.MockDisk) {
	t.Helper()
	dir := t.TempDir()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second // never used for mock, but keep tests fast
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rt := hypervisor.NewMockRuntime()
	disk := hypervisor.NewMockDisk()
	return manager.New(cfg, st, rt, disk, nil), rt, disk
}

func TestCreateEphemeralDefault(t *testing.T) {
	t.Parallel()
	m, rt, disk := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "sbox-1" {
		t.Fatalf("name %s", inst.Name)
	}
	if inst.Persistent {
		t.Fatal("should be ephemeral by default")
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status %s", inst.Status)
	}
	if !rt.Running(inst) {
		t.Fatal("runtime not running")
	}
	if disk.CloneCount() != 1 {
		t.Fatalf("clones %d", disk.CloneCount())
	}
	if !manager.DiskExists(inst.DiskPath) {
		t.Fatal("disk missing")
	}
}

func TestCreatePersistentAndShutdownKeepsMeta(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusStopped {
		t.Fatalf("status %s", got.Status)
	}
	if !manager.DiskExists(got.DiskPath) {
		t.Fatal("persistent disk should remain")
	}
}

func TestPersistentShutdownThenStart(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	if rt.Running(inst) {
		t.Fatal("should not be running after shutdown")
	}
	got, err := m.Start(context.Background(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusRunning {
		t.Fatalf("status %s", got.Status)
	}
	if !rt.Running(got) {
		t.Fatal("runtime should be running after start")
	}
	if got.PID == 0 {
		t.Fatal("expected pid after start")
	}
	// second start while running should fail
	if _, err := m.Start(context.Background(), "lab"); err == nil {
		t.Fatal("expected already running error")
	}
}

func TestStopAliasEphemeralDeletes(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(inst.Name); err == nil {
		t.Fatal("ephemeral should be gone after stop")
	}
}

func TestShutdownEphemeralDeletes(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(inst.Name); err == nil {
		t.Fatal("ephemeral should be gone after shutdown")
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	path := inst.DiskPath
	if err := m.Delete(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	if manager.DiskExists(path) {
		t.Fatal("disk should be removed")
	}
}

func TestCleanupEphemeral(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	_, _ = m.Create(context.Background(), vm.CreateOpts{})
	_, _ = m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "keep"})
	if err := m.CleanupEphemeral(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "keep" {
		t.Fatalf("list %+v", list)
	}
}

func TestDuplicateName(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "a"}); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestInvalidName(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "Bad_Name"}); err == nil {
		t.Fatal("expected invalid name")
	}
}

func TestListRefreshesStopped(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// simulate hypervisor exit without going through Shutdown
	rt.ForceDead(inst.Name)
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != vm.StatusStopped {
		t.Fatalf("want stopped, got %+v", list)
	}
}


func TestCreateEmitsEvents(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	var phases []string
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "ev1",
		OnEvent: func(ev vm.CreateEvent) {
			phases = append(phases, ev.Phase)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "ev1" {
		t.Fatalf("name %s", inst.Name)
	}
	want := []string{
		vm.PhaseImage,
		vm.PhaseDisk,
		vm.PhaseSeed,
		vm.PhaseQEMU,
		vm.PhaseWaitSSH,
		vm.PhaseReady,
	}
	if len(phases) != len(want) {
		t.Fatalf("phases %v want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phase[%d]=%s want %s (all %v)", i, phases[i], want[i], phases)
		}
	}
}

func TestResourceCapMaxVMs(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxVMs = 2
	cfg.MaxCPUsTotal = 0 // unlimited so we hit max_vms first
	cfg.MaxMemoryMBTotal = 0
	m, _, _ := testManagerCfg(t, cfg)

	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "b"}); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(context.Background(), vm.CreateOpts{Name: "c"})
	if err == nil {
		t.Fatal("expected max_vms cap error")
	}
	if !strings.Contains(err.Error(), "resource cap: max_vms is 2") {
		t.Fatalf("error %v", err)
	}
	if !strings.Contains(err.Error(), "already 2 running") {
		t.Fatalf("error %v", err)
	}
}

func TestResourceCapPerVMMemory(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxMemoryMBPerVM = 1024
	cfg.MaxVMs = 0
	cfg.MaxCPUsTotal = 0
	cfg.MaxMemoryMBTotal = 0
	m, _, _ := testManagerCfg(t, cfg)

	_, err := m.Create(context.Background(), vm.CreateOpts{Name: "big", MemoryMB: 2048})
	if err == nil {
		t.Fatal("expected max_memory_mb_per_vm cap error")
	}
	if !strings.Contains(err.Error(), "resource cap: max_memory_mb_per_vm is 1024") {
		t.Fatalf("error %v", err)
	}
	// under the per-VM cap still works
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "ok", MemoryMB: 512}); err != nil {
		t.Fatal(err)
	}
}

func TestResourceCapPerVMCPUs(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxCPUsPerVM = 2
	cfg.MaxVMs = 0
	cfg.MaxCPUsTotal = 0
	cfg.MaxMemoryMBTotal = 0
	m, _, _ := testManagerCfg(t, cfg)

	_, err := m.Create(context.Background(), vm.CreateOpts{Name: "wide", CPUs: 4})
	if err == nil {
		t.Fatal("expected max_cpus_per_vm cap error")
	}
	if !strings.Contains(err.Error(), "resource cap: max_cpus_per_vm is 2") {
		t.Fatalf("error %v", err)
	}
}

func TestResourceCapStoppedDoesNotCount(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxVMs = 1
	cfg.MaxCPUsTotal = 0
	cfg.MaxMemoryMBTotal = 0
	m, _, _ := testManagerCfg(t, cfg)

	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"}); err != nil {
		t.Fatal(err)
	}
	// while running, second create must fail
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "other"}); err == nil {
		t.Fatal("expected max_vms while first is running")
	}
	if err := m.Shutdown(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	// stopped VM does not count — new create succeeds
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "other"}); err != nil {
		t.Fatal(err)
	}
}

func TestResourceCapStartRespectsMaxVMs(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxVMs = 1
	cfg.MaxCPUsTotal = 0
	cfg.MaxMemoryMBTotal = 0
	m, _, _ := testManagerCfg(t, cfg)

	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "b"}); err != nil {
		t.Fatal(err)
	}
	// b is running; starting a should hit max_vms
	_, err := m.Start(context.Background(), "a")
	if err == nil {
		t.Fatal("expected max_vms on start")
	}
	if !strings.Contains(err.Error(), "resource cap: max_vms is 1") {
		t.Fatalf("error %v", err)
	}
}

func TestResourceCapTotalCPUs(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxVMs = 0
	cfg.MaxCPUsTotal = 4
	cfg.MaxMemoryMBTotal = 0
	cfg.MaxCPUsPerVM = 0
	cfg.DefaultCPUs = 2
	m, _, _ := testManagerCfg(t, cfg)

	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "a", CPUs: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "b", CPUs: 2}); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(context.Background(), vm.CreateOpts{Name: "c", CPUs: 2})
	if err == nil {
		t.Fatal("expected max_cpus_total cap error")
	}
	if !strings.Contains(err.Error(), "resource cap: max_cpus_total is 4") {
		t.Fatalf("error %v", err)
	}
}

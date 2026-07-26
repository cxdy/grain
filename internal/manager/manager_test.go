package manager_test

import (
	"context"
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
	dir := t.TempDir()
	cfg := config.Defaults()
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

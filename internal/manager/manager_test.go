package manager_test

import (
	"context"
	"os"
	"path/filepath"
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

func TestCreateWithForwardsPersistsHostPorts(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "web",
		Forwards: []vm.PortForward{
			{HostPort: 0, GuestPort: 80},
			{HostPort: 8443, GuestPort: 443, Proto: "tcp"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Forwards) != 2 {
		t.Fatalf("forwards %v", inst.Forwards)
	}
	if inst.Forwards[0].HostPort < 1024 {
		t.Fatalf("auto host port not allocated: %d", inst.Forwards[0].HostPort)
	}
	if inst.Forwards[0].GuestPort != 80 {
		t.Fatalf("guest %d", inst.Forwards[0].GuestPort)
	}
	if inst.Forwards[1].HostPort != 8443 || inst.Forwards[1].GuestPort != 443 {
		t.Fatalf("fixed forward %+v", inst.Forwards[1])
	}

	// reloaded from store should keep allocated ports
	got, err := m.Get("web")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Forwards) != 2 {
		t.Fatalf("persisted forwards %v", got.Forwards)
	}
	if got.Forwards[0].HostPort != inst.Forwards[0].HostPort {
		t.Fatalf("persisted host port %d want %d", got.Forwards[0].HostPort, inst.Forwards[0].HostPort)
	}
	if got.Forwards[1].HostPort != 8443 {
		t.Fatalf("persisted fixed %d", got.Forwards[1].HostPort)
	}
}

func TestCreateRejectsPrivilegedHostPort(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "bad",
		Forwards: []vm.PortForward{
			{HostPort: 80, GuestPort: 80},
		},
	})
	if err == nil {
		t.Fatal("expected privileged host port error")
	}
}

func TestStartReappliesForwards(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Persistent: true,
		Name:       "svc",
		Forwards: []vm.PortForward{
			{HostPort: 18080, GuestPort: 8080},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	host := inst.Forwards[0].HostPort
	if err := m.Shutdown(context.Background(), "svc"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Start(context.Background(), "svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Forwards) != 1 || got.Forwards[0].HostPort != host {
		t.Fatalf("forwards after start %+v want host %d", got.Forwards, host)
	}
	if got.Forwards[0].GuestPort != 8080 {
		t.Fatalf("guest %d", got.Forwards[0].GuestPort)
	}
}

func TestCreateStoresMounts(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	hostDir := t.TempDir()
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "mnt1",
		Mounts: []vm.Mount{
			{Host: hostDir, Guest: "/mnt/src"},
			{Host: hostDir, Guest: "/data"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Mounts) != 2 {
		t.Fatalf("mounts %v", inst.Mounts)
	}
	if inst.Mounts[0].Tag != "grain0" || inst.Mounts[1].Tag != "grain1" {
		t.Fatalf("tags %q %q", inst.Mounts[0].Tag, inst.Mounts[1].Tag)
	}
	if inst.Mounts[0].Guest != "/mnt/src" || inst.Mounts[1].Guest != "/data" {
		t.Fatalf("guests %+v", inst.Mounts)
	}
	// host should be absolute
	if !filepath.IsAbs(inst.Mounts[0].Host) {
		t.Fatalf("host not abs: %s", inst.Mounts[0].Host)
	}
	// persisted
	got, err := m.Get("mnt1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Mounts) != 2 || got.Mounts[0].Tag != "grain0" {
		t.Fatalf("persisted mounts %+v", got.Mounts)
	}
}

func TestCreateRejectsNonDirMount(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name:   "badmnt",
		Mounts: []vm.Mount{{Host: f, Guest: "/mnt/x"}},
	})
	if err == nil {
		t.Fatal("expected non-directory error")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err %v", err)
	}
}

func TestCreateRejectsMissingMountHost(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name:   "nomnt",
		Mounts: []vm.Mount{{Host: filepath.Join(t.TempDir(), "nope"), Guest: "/mnt/x"}},
	})
	if err == nil {
		t.Fatal("expected missing host error")
	}
}

func TestStartReappliesMounts(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	hostDir := t.TempDir()
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Persistent: true,
		Name:       "mntsvc",
		Mounts:     []vm.Mount{{Host: hostDir, Guest: "/work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tag := inst.Mounts[0].Tag
	host := inst.Mounts[0].Host
	if err := m.Shutdown(context.Background(), "mntsvc"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Start(context.Background(), "mntsvc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Tag != tag || got.Mounts[0].Host != host {
		t.Fatalf("mounts after start %+v", got.Mounts)
	}
	if got.Mounts[0].Guest != "/work" {
		t.Fatalf("guest %s", got.Mounts[0].Guest)
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

func TestNormalizeWaitMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", vm.WaitSSH, false},
		{"ssh", vm.WaitSSH, false},
		{"agent", vm.WaitAgent, false},
		{"userdata", vm.WaitUserdata, false},
		{"nope", "", true},
		{"SSH", "", true},
	}
	for _, tc := range cases {
		got, err := manager.NormalizeWaitMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("NormalizeWaitMode(%q) want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeWaitMode(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeWaitMode(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCreateWaitAgentMock(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	var phases []string
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "agent-wait",
		WaitMode: vm.WaitAgent,
		OnEvent: func(ev vm.CreateEvent) {
			phases = append(phases, ev.Phase)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status %s", inst.Status)
	}
	joined := strings.Join(phases, ",")
	if !strings.Contains(joined, vm.PhaseWaitAgent) {
		t.Fatalf("want wait_agent phase in %v", phases)
	}
	if !strings.Contains(joined, vm.PhaseReady) {
		t.Fatalf("want ready in %v", phases)
	}
	if strings.Contains(joined, vm.PhaseWaitSSH) {
		t.Fatalf("unexpected wait_ssh in agent mock path: %v", phases)
	}
}

func TestCreateWaitUserdataMock(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	var phases []string
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "ud-wait",
		WaitMode: vm.WaitUserdata,
		OnEvent: func(ev vm.CreateEvent) {
			phases = append(phases, ev.Phase)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst == nil {
		t.Fatal("nil instance")
	}
	joined := strings.Join(phases, ",")
	for _, p := range []string{vm.PhaseWaitAgent, vm.PhaseUserdata, vm.PhaseReady} {
		if !strings.Contains(joined, p) {
			t.Fatalf("missing phase %s in %v", p, phases)
		}
	}
}

func TestCreateRejectsInvalidWaitMode(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "bad-wait",
		WaitMode: "nope",
	})
	if err == nil {
		t.Fatal("expected invalid wait mode error")
	}
	if !strings.Contains(err.Error(), "invalid wait mode") {
		t.Fatalf("error %v", err)
	}
}

func TestCreateWaitSSHDefaultPhases(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	var phases []string
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "ssh-wait",
		WaitMode: vm.WaitSSH,
		OnEvent: func(ev vm.CreateEvent) {
			phases = append(phases, ev.Phase)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(phases, ",")
	if !strings.Contains(joined, vm.PhaseWaitSSH) {
		t.Fatalf("want wait_ssh in %v", phases)
	}
}


func TestPauseResumeMock(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusPaused {
		t.Fatalf("status %s want paused", got.Status)
	}
	if !rt.Paused("lab") {
		t.Fatal("runtime should track paused")
	}
	if !rt.Running(got) {
		t.Fatal("process should still be alive while paused")
	}
	if err := m.Pause(context.Background(), "lab"); err == nil {
		t.Fatal("expected already paused")
	}
	if _, err := m.Start(context.Background(), "lab"); err == nil {
		t.Fatal("expected start fail while paused")
	}
	if err := m.Resume(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	got, err = m.Get("lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusRunning {
		t.Fatalf("status %s want running", got.Status)
	}
	if rt.Paused("lab") {
		t.Fatal("runtime should not be paused after resume")
	}
	if err := m.Resume(context.Background(), "lab"); err == nil {
		t.Fatal("expected not paused error")
	}
}

func TestAddRemoveLiveForwardMock(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	lf, err := m.AddForward(context.Background(), inst.Name, 18080, 80)
	if err != nil {
		t.Fatal(err)
	}
	if lf.HostPort != 18080 || lf.GuestPort != 80 {
		t.Fatalf("forward %+v", lf)
	}
	got, err := m.Get("svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LiveForwards) != 1 {
		t.Fatalf("live forwards %v", got.LiveForwards)
	}
	if _, err := m.AddForward(context.Background(), "svc", 18080, 443); err == nil {
		t.Fatal("expected duplicate host port error")
	}
	lf2, err := m.AddForward(context.Background(), "svc", 0, 443)
	if err != nil {
		t.Fatal(err)
	}
	if lf2.HostPort < 1024 {
		t.Fatalf("auto host port %d", lf2.HostPort)
	}
	if err := m.RemoveForward(context.Background(), "svc", 18080); err != nil {
		t.Fatal(err)
	}
	got, err = m.Get("svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LiveForwards) != 1 || got.LiveForwards[0].HostPort != lf2.HostPort {
		t.Fatalf("after rm %+v", got.LiveForwards)
	}
	if err := m.Shutdown(context.Background(), "svc"); err != nil {
		t.Fatal(err)
	}
	got, err = m.Get("svc")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.LiveForwards) != 0 {
		t.Fatalf("expected cleared live forwards, got %+v", got.LiveForwards)
	}
}

func TestResourceCapPausedCounts(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxVMs = 1
	cfg.MaxCPUsTotal = 0
	cfg.MaxMemoryMBTotal = 0
	m, _, _ := testManagerCfg(t, cfg)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "other"}); err == nil {
		t.Fatal("expected max_vms while first is paused")
	}
}

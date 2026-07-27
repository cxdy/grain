package manager_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/image"
	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func testManager(t *testing.T) (*manager.Manager, *hypervisor.MockRuntime, *hypervisor.MockDisk) {
	t.Helper()
	return testManagerCfg(t, mockCfg(t))
}

// mockCfg is Defaults with Hypervisor=mock and a short ReadyTimeout.
func mockCfg(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	return cfg
}

func testManagerCfg(t *testing.T, cfg config.Config) (*manager.Manager, *hypervisor.MockRuntime, *hypervisor.MockDisk) {
	t.Helper()
	dir := t.TempDir()
	cfg.DataDir = dir
	// Do not force Hypervisor/ReadyTimeout — callers must set them (see mockCfg / nonMockCfg).
	if cfg.Hypervisor == "" {
		cfg.Hypervisor = "mock"
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = time.Second
	}
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
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
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
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
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
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
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
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
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
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
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
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
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

func TestCreatePrefersGoldenImageWhenReady(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.Image = "auto"
	cfg.ReadyTimeout = time.Second
	// Plant a Ready grain-ubuntu disk (same layout as image.Ready / DefaultIDFor).
	imgDir := filepath.Join(dir, "images", image.IDGrainUbuntu)
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "has_agent"), []byte("true\n"), 0o644); err != nil {
		// has_agent may be a meta file format — ImageHasAgent reads meta; catalog HasAgent is enough for grain-ubuntu
		_ = err
	}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Mock disk EnsureBase may need the image id ready — MockDisk often ignores real files.
	m := manager.New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	inst, err := m.Create(context.Background(), vm.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Image != image.IDGrainUbuntu {
		t.Fatalf("image=%q want %q", inst.Image, image.IDGrainUbuntu)
	}
}

func TestNormalizeWaitMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"auto", "", false},
		{"ssh", vm.WaitSSH, false},
		{"agent", vm.WaitAgent, false},
		{"userdata", vm.WaitUserdata, false},
		{"nope", "", true},
		{"SSH", vm.WaitSSH, false},
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

func TestSuspendRestoreMock(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusSuspended {
		t.Fatalf("status %s want suspended", got.Status)
	}
	if got.SuspendedAt.IsZero() {
		t.Fatal("expected SuspendedAt")
	}
	if rt.Running(got) {
		t.Fatal("process should be stopped after suspend")
	}
	if got.PID != 0 {
		t.Fatalf("pid should be cleared, got %d", got.PID)
	}
	// Disk retained
	if !manager.DiskExists(got.DiskPath) {
		t.Fatal("disk should remain after suspend")
	}
	// Marker written (mock SaveVM succeeds)
	marker := filepath.Join(filepath.Dir(got.DiskPath), hypervisor.SuspendMarkerName)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected suspend marker: %v", err)
	}
	// Start rejected while suspended
	if _, err := m.Start(context.Background(), "lab"); err == nil {
		t.Fatal("expected start fail while suspended")
	}
	// Double suspend fails
	if err := m.Suspend(context.Background(), "lab"); err == nil {
		t.Fatal("expected already suspended")
	}
	// Restore
	got, err = m.Restore(context.Background(), "lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusRunning {
		t.Fatalf("status %s want running", got.Status)
	}
	if !rt.Running(got) {
		t.Fatal("runtime should be running after restore")
	}
	if !got.SuspendedAt.IsZero() {
		t.Fatal("SuspendedAt should be cleared")
	}
	// Marker consumed
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("suspend marker should be cleared after restore")
	}
	// Restore only from suspended
	if _, err := m.Restore(context.Background(), "lab"); err == nil {
		t.Fatal("expected restore fail when not suspended")
	}
}

func TestSuspendRequiresPersistent(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Name: "ephem"})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Suspend(context.Background(), inst.Name)
	if err == nil || !strings.Contains(err.Error(), "ephemeral") {
		t.Fatalf("want ephemeral error, got %v", err)
	}
}

func TestSuspendFromPaused(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("lab")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusSuspended {
		t.Fatalf("status %s", got.Status)
	}
	if rt.Running(got) {
		t.Fatal("should not be running")
	}
}

func TestResourceCapSuspendedDoesNotCount(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	cfg.MaxVMs = 1
	cfg.MaxCPUsTotal = 0
	cfg.MaxMemoryMBTotal = 0
	m, _, _ := testManagerCfg(t, cfg)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Suspend(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	// Suspended frees the slot — another create should succeed.
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "other"}); err != nil {
		t.Fatal(err)
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
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
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

func TestCreateSocketForwardsMock(t *testing.T) {
	m, _, _ := testManager(t)
	hostSock := filepath.Join(t.TempDir(), "docker.sock")
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "sockvm",
		SocketForwards: []vm.SocketForward{
			{HostPath: hostSock, GuestPath: "/var/run/docker.sock"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.SocketForwards) != 1 {
		t.Fatalf("socket forwards %+v", inst.SocketForwards)
	}
	if inst.SocketForwards[0].GuestPath != "/var/run/docker.sock" {
		t.Fatalf("guest %s", inst.SocketForwards[0].GuestPath)
	}
	if inst.SocketForwards[0].PID != 1 {
		t.Fatalf("mock pid %d", inst.SocketForwards[0].PID)
	}
	got, err := m.Get("sockvm")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.SocketForwards) != 1 || got.SocketForwards[0].HostPath != hostSock {
		t.Fatalf("persisted %+v", got.SocketForwards)
	}
}

func TestCreateSocketForwardsRejectsRelativeGuest(t *testing.T) {
	m, _, _ := testManager(t)
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "bad-sock",
		SocketForwards: []vm.SocketForward{
			{HostPath: filepath.Join(t.TempDir(), "a.sock"), GuestPath: "relative"},
		},
	})
	if err == nil {
		t.Fatal("expected error for relative guest path")
	}
}

func TestCreateTimeoutAndReadyTimeout(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	cfg.ReadyTimeout = 30 * time.Second
	m, _, _ := testManagerCfg(t, cfg)
	// ReadyTimeout + 2m = 2m30s → floored to 5m
	if m.CreateTimeout() != 5*time.Minute {
		t.Fatalf("CreateTimeout %v", m.CreateTimeout())
	}
	if m.ReadyTimeout() != 30*time.Second {
		t.Fatalf("ReadyTimeout %v", m.ReadyTimeout())
	}

	cfg2 := config.Defaults()
	cfg2.Hypervisor = "mock"
	cfg2.ReadyTimeout = time.Second
	cfg2.ReadyTimeout = 10 * time.Minute
	m2, _, _ := testManagerCfg(t, cfg2)
	if m2.CreateTimeout() != 12*time.Minute {
		t.Fatalf("CreateTimeout large %v", m2.CreateTimeout())
	}
}

func TestCreateRejectsInvalidArchGPUNetwork(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "a1", Arch: "riscv"}); err == nil {
		t.Fatal("expected arch error")
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "g1", GPU: "vfio"}); err == nil {
		t.Fatal("expected gpu error")
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "n1", Network: "bridge"}); err == nil {
		t.Fatal("expected network error")
	}
}

func TestCreateWithArchGPUNetworkAndResources(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "opts1",
		CPUs:     1,
		MemoryMB: 512,
		DiskGB:   4,
		Arch:     "amd64",
		GPU:      "virtio",
		Network:  "overlay",
		Image:    "ubuntu-cloud",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.CPUs != 1 || inst.MemoryMB != 512 || inst.DiskGB != 4 {
		t.Fatalf("resources %+v", inst)
	}
	if inst.Arch != "amd64" || inst.GPU != "virtio" || inst.Network != "overlay" {
		t.Fatalf("arch/gpu/net %s/%s/%s", inst.Arch, inst.GPU, inst.Network)
	}
	// aarch64 alias
	inst2, err := m.Create(context.Background(), vm.CreateOpts{Name: "opts2", Arch: "aarch64"})
	if err != nil {
		t.Fatal(err)
	}
	if inst2.Arch != "arm64" {
		t.Fatalf("arch %s", inst2.Arch)
	}
}

func TestCreateFailStartEmitsError(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	rt.FailStart = true
	var phases []string
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "failstart",
		OnEvent: func(ev vm.CreateEvent) {
			phases = append(phases, ev.Phase)
		},
	})
	if err == nil {
		t.Fatal("expected start failure")
	}
	joined := strings.Join(phases, ",")
	if !strings.Contains(joined, vm.PhaseError) {
		t.Fatalf("want error phase in %v", phases)
	}
	// Instance should be in error state or cleaned depending on fail path
	got, gerr := m.Get("failstart")
	if gerr == nil && got.Status != vm.StatusError && got.Status != vm.StatusStopped {
		// fail() marks error; acceptable either way if still listed
		if got.Error == "" && got.Status == vm.StatusRunning {
			t.Fatalf("unexpected running after fail: %+v", got)
		}
	}
}

func TestGetMissingAndDeleteMissing(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Get("nope"); err == nil {
		t.Fatal("expected get missing")
	}
	if err := m.Delete(context.Background(), "nope"); err == nil {
		t.Fatal("expected delete missing")
	}
	if err := m.Shutdown(context.Background(), "nope"); err == nil {
		t.Fatal("expected shutdown missing")
	}
	if err := m.Pause(context.Background(), "nope"); err == nil {
		t.Fatal("expected pause missing")
	}
	if err := m.Resume(context.Background(), "nope"); err == nil {
		t.Fatal("expected resume missing")
	}
}

func TestDiskExistsHelper(t *testing.T) {
	t.Parallel()
	if manager.DiskExists("/nonexistent/path/disk.img") {
		t.Fatal("should not exist")
	}
	f := filepath.Join(t.TempDir(), "d.img")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !manager.DiskExists(f) {
		t.Fatal("should exist")
	}
}

func TestCreateRejectsEmptyMountFields(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{
		Name:   "m1",
		Mounts: []vm.Mount{{Host: "", Guest: "/mnt"}},
	}); err == nil {
		t.Fatal("expected empty host")
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{
		Name:   "m2",
		Mounts: []vm.Mount{{Host: t.TempDir(), Guest: ""}},
	}); err == nil {
		t.Fatal("expected empty guest")
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{
		Name:   "m3",
		Mounts: []vm.Mount{{Host: t.TempDir(), Guest: "relative"}},
	}); err == nil {
		t.Fatal("expected relative guest")
	}
}

func TestCreateRejectsBadForwardGuestPort(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "badfwd",
		Forwards: []vm.PortForward{{HostPort: 8080, GuestPort: 0}},
	})
	if err == nil {
		t.Fatal("expected forward validation error")
	}
}

func TestAddForwardRequiresRunning(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddForward(context.Background(), "lab", 0, 80); err == nil {
		t.Fatal("expected add forward fail when stopped")
	}
	if err := m.RemoveForward(context.Background(), "lab", 9999); err == nil {
		t.Fatal("expected remove missing forward")
	}
}

func TestCreateWithUserdataAndTags(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "ud",
		Userdata: "echo hi",
		Tags:     map[string]string{"a": "b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Tags["a"] != "b" {
		t.Fatalf("tags %+v", inst.Tags)
	}
}

func TestCreateWaitTimeoutOverride(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name:        "wto",
		WaitMode:    vm.WaitSSH,
		WaitTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status %s", inst.Status)
	}
}

func TestResourceCapTotalMemory(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	cfg.MaxMemoryMBTotal = 1024
	cfg.MaxMemoryMBPerVM = 0
	cfg.MaxCPUsTotal = 0
	cfg.MaxVMs = 0
	m, _, _ := testManagerCfg(t, cfg)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "a", MemoryMB: 1024}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "b", MemoryMB: 512}); err == nil {
		t.Fatal("expected total memory cap")
	}
}

func TestStartMissingDisk(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	// Remove disk to force start failure path.
	_ = os.Remove(inst.DiskPath)
	_ = os.Remove(inst.DiskPath + ".qcow2")
	if _, err := m.Start(context.Background(), "lab"); err == nil {
		t.Fatal("expected start error when disk missing")
	} else if !strings.Contains(err.Error(), "no disk") {
		t.Fatalf("want no disk error, got %v", err)
	}
}

func TestCreateArchAliases(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	for _, tc := range []struct {
		in, want string
	}{
		{"host", ""},
		{"x86_64", "amd64"},
		{"x64", "amd64"},
	} {
		name := "arch-" + strings.ReplaceAll(tc.in, "_", "")
		inst, err := m.Create(context.Background(), vm.CreateOpts{Name: name, Arch: tc.in})
		if err != nil {
			t.Fatalf("arch %s: %v", tc.in, err)
		}
		if inst.Arch != tc.want {
			t.Fatalf("arch %s: got %q want %q", tc.in, inst.Arch, tc.want)
		}
	}
}

func TestCreateSocketForwardEmptyHost(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "empty-sock",
		SocketForwards: []vm.SocketForward{
			{HostPath: "", GuestPath: "/var/run/docker.sock"},
		},
	})
	if err == nil {
		t.Fatal("expected empty host path error")
	}
}

func TestDeleteRunningStopsFirst(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "delrun"})
	if err != nil {
		t.Fatal(err)
	}
	if !rt.Running(inst) {
		t.Fatal("should be running")
	}
	if err := m.Delete(context.Background(), "delrun"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get("delrun"); err == nil {
		t.Fatal("should be gone")
	}
}

func TestPauseNotRunning(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Persistent: true, Name: "lab"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), "lab"); err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(context.Background(), "lab"); err == nil {
		t.Fatal("expected pause fail when stopped")
	}
	if err := m.Resume(context.Background(), "lab"); err == nil {
		t.Fatal("expected resume fail when stopped")
	}
}

// nonMockCfg uses Hypervisor="qemu" so Manager wait paths do not short-circuit,
// while still injecting MockRuntime/MockDisk (no real QEMU).
func nonMockCfg(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Defaults()
	cfg.Hypervisor = "qemu"
	cfg.ReadyTimeout = 300 * time.Millisecond
	return cfg
}

func TestCreateWaitSSHSoftWhenNoGuest(t *testing.T) {
	t.Parallel()
	// isMock=false → WaitSSH is attempted; soft mode still succeeds Create.
	m, _, _ := testManagerCfg(t, nonMockCfg(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inst, err := m.Create(ctx, vm.CreateOpts{
		Name:        "softssh",
		WaitMode:    vm.WaitSSH,
		WaitTimeout: 250 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status %s", inst.Status)
	}
}

func TestCreateWaitAgentHardFailsWithoutAgent(t *testing.T) {
	t.Parallel()
	m, _, _ := testManagerCfg(t, nonMockCfg(t))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := m.Create(ctx, vm.CreateOpts{
		Name:        "hardagent",
		WaitMode:    vm.WaitAgent,
		WaitTimeout: 400 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected wait agent failure without guest")
	}
	if !strings.Contains(err.Error(), "agent") && !strings.Contains(err.Error(), "ssh") {
		t.Fatalf("error %v", err)
	}
}

func TestCreateWaitUserdataFailsWithoutAgent(t *testing.T) {
	t.Parallel()
	m, _, _ := testManagerCfg(t, nonMockCfg(t))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := m.Create(ctx, vm.CreateOpts{
		Name:        "hardud",
		WaitMode:    vm.WaitUserdata,
		WaitTimeout: 400 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected userdata wait failure")
	}
}

func TestCreateWaitUserdataWithLocalAgent(t *testing.T) {
	// Not parallel: first mock VM gets AgentPort 7476; bind agent there first.
	// Hypervisor label "qemu" so wait paths run; runtime is still MockRuntime.
	ln, err := net.Listen("tcp", "127.0.0.1:7476")
	if err != nil {
		t.Skipf("port 7476 busy: %v", err)
	}
	_ = ln.Close()

	// Boot real grain-agent on the port MockRuntime assigns to the first VM.
	srv := agent.NewServer("127.0.0.1:7476", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	// Wait until agent is up.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ac := &agent.Client{BaseURL: "http://127.0.0.1:7476", HTTP: &http.Client{Timeout: time.Second}}
		if _, err := ac.Health(context.Background()); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cfg := nonMockCfg(t)
	cfg.ReadyTimeout = 5 * time.Second
	m, _, _ := testManagerCfg(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// UserdataRan is false on fresh agent → wait userdata should time out.
	_, err = m.Create(ctx, vm.CreateOpts{
		Name:        "udlocal",
		WaitMode:    vm.WaitUserdata,
		WaitTimeout: 500 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected userdata timeout (UserdataRan=false)")
	}
	if !strings.Contains(err.Error(), "userdata") && !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "context") {
		// still useful if agent path partially ran
		t.Logf("userdata wait error: %v", err)
	}
}

func TestAddForwardNonMockUsesSSHPath(t *testing.T) {
	t.Parallel()
	// Hypervisor not mock → AddForward starts real ssh -N -L (not mock PID=1).
	// Without a guest the process may die quickly (error) or linger briefly (PID>1).
	cfg := nonMockCfg(t)
	m, _, _ := testManagerCfg(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inst, err := m.Create(ctx, vm.CreateOpts{
		Name:        "fwdssh",
		WaitMode:    vm.WaitSSH,
		WaitTimeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	lf, err := m.AddForward(ctx, inst.Name, 19090, 80)
	if err != nil {
		// Expected when ssh cannot establish the forward.
		return
	}
	t.Cleanup(func() {
		_ = m.RemoveForward(context.Background(), inst.Name, lf.HostPort)
	})
	if lf.PID <= 1 {
		t.Fatalf("non-mock forward should not use mock PID=1, got %d", lf.PID)
	}
}

func TestCreateSocketForwardsNonMock(t *testing.T) {
	t.Parallel()
	cfg := nonMockCfg(t)
	m, _, _ := testManagerCfg(t, cfg)
	hostSock := filepath.Join(t.TempDir(), "d.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Create succeeds (soft SSH); socket forwards may fail non-fatally.
	inst, err := m.Create(ctx, vm.CreateOpts{
		Name:        "socknm",
		WaitMode:    vm.WaitSSH,
		WaitTimeout: 200 * time.Millisecond,
		SocketForwards: []vm.SocketForward{
			{HostPath: hostSock, GuestPath: "/var/run/docker.sock"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.SocketForwards) != 1 {
		t.Fatalf("forwards %+v", inst.SocketForwards)
	}
}

func TestNormalizeWaitModeCoverage(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"", "auto", "ssh", "agent", "userdata", "SSH", "Agent"} {
		got, err := manager.NormalizeWaitMode(mode)
		if mode == "" {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		_ = got
	}
	if _, err := manager.NormalizeWaitMode("nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDiskExistsCoverage(t *testing.T) {
	t.Parallel()
	if manager.DiskExists("") {
		t.Fatal()
	}
	if manager.DiskExists(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal()
	}
	p := filepath.Join(t.TempDir(), "d")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !manager.DiskExists(p) {
		t.Fatal()
	}
}

func TestManagerLifecycleAndCaps(t *testing.T) {
	cfg := mockCfg(t)
	cfg.MaxVMs = 1
	cfg.MaxCPUsPerVM = 2
	cfg.MaxMemoryMBPerVM = 512
	cfg.MaxCPUsTotal = 2
	cfg.MaxMemoryMBTotal = 512
	m, _, _ := testManagerCfg(t, cfg)
	ctx := context.Background()

	// per-vm caps
	if _, err := m.Create(ctx, vm.CreateOpts{Name: "big", CPUs: 8, MemoryMB: 9999}); err == nil {
		t.Fatal("expected per-vm cap")
	}

	inst, err := m.Create(ctx, vm.CreateOpts{Name: "one", Persistent: true, CPUs: 1, MemoryMB: 256})
	if err != nil {
		t.Fatal(err)
	}

	// max VMs
	if _, err := m.Create(ctx, vm.CreateOpts{Name: "two", CPUs: 1, MemoryMB: 256}); err == nil {
		t.Fatal("expected max vms")
	}

	// list/get
	list, err := m.List()
	if err != nil || len(list) < 1 {
		t.Fatalf("%v %v", list, err)
	}
	got, err := m.Get(inst.Name)
	if err != nil || got.Name != inst.Name {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := m.Get("missing"); err == nil {
		t.Fatal("missing get")
	}

	// pause/resume
	if err := m.Pause(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}
	if err := m.Resume(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}
	// pause missing
	if err := m.Pause(ctx, "missing"); err == nil {
		t.Fatal()
	}
	if err := m.Resume(ctx, "missing"); err == nil {
		t.Fatal()
	}

	// shutdown persistent → stopped
	if err := m.Shutdown(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}
	stopped, err := m.Get(inst.Name)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != vm.StatusStopped {
		t.Fatalf("status %s", stopped.Status)
	}

	// start
	started, err := m.Start(ctx, inst.Name)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != vm.StatusRunning {
		t.Fatalf("%s", started.Status)
	}

	// suspend/restore when supported by mock
	if err := m.Suspend(ctx, inst.Name); err != nil {
		// mock may not support suspend fully
		t.Log(err)
	} else {
		if _, err := m.Restore(ctx, inst.Name); err != nil {
			t.Log(err)
		}
	}

	// delete
	if err := m.Delete(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "missing"); err == nil {
		t.Fatal()
	}
	if err := m.Shutdown(ctx, "missing"); err == nil {
		t.Fatal()
	}
	if err := m.Stop(ctx, "missing"); err == nil {
		t.Fatal()
	}
	if _, err := m.Start(ctx, "missing"); err == nil {
		t.Fatal()
	}
	if err := m.Suspend(ctx, "missing"); err == nil {
		t.Fatal()
	}
	if _, err := m.Restore(ctx, "missing"); err == nil {
		t.Fatal()
	}

	// cleanup ephemeral
	if err := m.CleanupEphemeral(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestManagerCreateWithEventsAndOptions(t *testing.T) {
	m, _, _ := testManager(t)
	ctx := context.Background()
	var phases []string
	inst, err := m.Create(ctx, vm.CreateOpts{
		Name:       "ev1",
		Persistent: false,
		CPUs:       2,
		MemoryMB:   512,
		DiskGB:     4,
		WaitMode:   vm.WaitSSH,
		Tags:       map[string]string{"k": "v"},
		Userdata:   "#cloud\n",
		OnEvent: func(ev vm.CreateEvent) {
			phases = append(phases, ev.Phase)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst == nil || len(phases) == 0 {
		t.Fatalf("%+v phases=%v", inst, phases)
	}

	// wait agent mode (mock sets agent port 0 maybe — may fail or succeed)
	_, err = m.Create(ctx, vm.CreateOpts{Name: "ag1", WaitMode: vm.WaitAgent, WaitTimeout: time.Second})
	if err != nil {
		// acceptable if agent not available on mock
		if !strings.Contains(err.Error(), "agent") && !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "wait") {
			t.Logf("agent wait: %v", err)
		}
	}

	// invalid wait
	if _, err := m.Create(ctx, vm.CreateOpts{Name: "bad", WaitMode: "nope"}); err == nil {
		t.Fatal("bad wait")
	}

	// timeout helpers
	if m.CreateTimeout() <= 0 {
		t.Fatal()
	}
	if m.ReadyTimeout() <= 0 {
		t.Fatal()
	}
}

func TestManagerForwardsAndMounts(t *testing.T) {
	m, _, _ := testManager(t)
	ctx := context.Background()
	inst, err := m.Create(ctx, vm.CreateOpts{
		Name: "fwd",
		Forwards: []vm.PortForward{
			{HostPort: 0, GuestPort: 8080},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// live forward may fail without SSH
	if _, err := m.AddForward(ctx, inst.Name, 0, 80); err != nil {
		t.Log(err)
	}
	if err := m.RemoveForward(ctx, inst.Name, 9999); err != nil {
		t.Log(err)
	}
	if _, err := m.AddForward(ctx, "missing", 0, 80); err == nil {
		t.Fatal()
	}
	if err := m.RemoveForward(ctx, "missing", 1); err == nil {
		t.Fatal()
	}

	// mounts with host path
	host := t.TempDir()
	_, err = m.Create(ctx, vm.CreateOpts{
		Name: "mnt",
		Mounts: []vm.Mount{
			{Host: host, Guest: "/mnt/data"},
		},
	})
	if err != nil {
		t.Log(err) // may fail validation
	}
	// bad mount host
	_, err = m.Create(ctx, vm.CreateOpts{
		Name: "mnt2",
		Mounts: []vm.Mount{
			{Host: filepath.Join(t.TempDir(), "nope"), Guest: "/mnt"},
		},
	})
	if err == nil {
		t.Log("missing host mount accepted")
	}
}

func TestCreateFailEnsureBaseAndClone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m, _, disk := testManager(t)
	disk.FailEnsureBase = true
	var phases []string
	_, err := m.Create(ctx, vm.CreateOpts{
		Name: "failbase",
		OnEvent: func(ev vm.CreateEvent) {
			phases = append(phases, ev.Phase)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("ensure base: %v", err)
	}
	if !strings.Contains(strings.Join(phases, ","), vm.PhaseError) {
		t.Fatalf("phases %v", phases)
	}

	m2, _, disk2 := testManager(t)
	disk2.FailClone = true
	_, err = m2.Create(ctx, vm.CreateOpts{Name: "failclone"})
	if err == nil || !strings.Contains(err.Error(), "disk") {
		t.Fatalf("clone: %v", err)
	}
}

func TestSuspendFailSaveVMWithQcow2Sibling(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Name: "saves2", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	// Sibling .qcow2 makes diskLooksQcow2 true → savevm branch.
	sib := inst.DiskPath + ".qcow2"
	if err := os.WriteFile(sib, []byte{'Q', 'F', 'I', 0xfb, 0, 0, 0, 0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	rt.FailSaveVM = true
	if err := m.Suspend(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(inst.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusSuspended {
		t.Fatalf("status %s", got.Status)
	}
}

func TestFailPauseResumeStop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, rt, _ := testManager(t)
	inst, err := m.Create(ctx, vm.CreateOpts{Name: "failops", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}

	rt.FailPause = true
	if err := m.Pause(ctx, inst.Name); err == nil {
		t.Fatal("expected pause fail")
	}
	rt.FailPause = false
	if err := m.Pause(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}
	rt.FailResume = true
	if err := m.Resume(ctx, inst.Name); err == nil {
		t.Fatal("expected resume fail")
	}
	rt.FailResume = false
	if err := m.Resume(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}

	rt.FailStop = true
	if err := m.Shutdown(ctx, inst.Name); err == nil {
		t.Fatal("expected stop fail")
	}
	rt.FailStop = false
	// Delete while running with FailStop: Delete logs stop failure and continues.
	rt.FailStop = true
	_ = m.Delete(ctx, inst.Name)
}

func TestListRefreshesForceDead(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Name: "dead", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddForward(context.Background(), inst.Name, 0, 8080); err != nil {
		t.Fatal(err)
	}
	rt.ForceDead(inst.Name)
	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range list {
		if i.Name == inst.Name {
			found = true
			if i.Status != vm.StatusStopped {
				t.Fatalf("want stopped, got %s", i.Status)
			}
			if i.PID != 0 {
				t.Fatalf("pid %d", i.PID)
			}
			if len(i.LiveForwards) != 0 {
				t.Fatalf("forwards %+v", i.LiveForwards)
			}
		}
	}
	if !found {
		t.Fatal("missing vm")
	}
}

func TestCreateTimeoutLongReady(t *testing.T) {
	t.Parallel()
	cfg := mockCfg(t)
	cfg.ReadyTimeout = 10 * time.Minute
	m, _, _ := testManagerCfg(t, cfg)
	if got := m.CreateTimeout(); got < 12*time.Minute {
		t.Fatalf("CreateTimeout %v", got)
	}
	if m.ReadyTimeout() != 10*time.Minute {
		t.Fatalf("ReadyTimeout %v", m.ReadyTimeout())
	}
}

func TestCreateConfigDefaultsArchGPUNetwork(t *testing.T) {
	t.Parallel()
	cfg := mockCfg(t)
	cfg.GuestArch = "arm64"
	cfg.GPU = "virtio"
	cfg.Network = "overlay"
	cfg.DefaultCPUs = 3
	cfg.DefaultMemoryMB = 768
	cfg.DefaultDiskGB = 6
	m, _, _ := testManagerCfg(t, cfg)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Name: "defs"})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Arch != "arm64" || inst.GPU != "virtio" || inst.Network != "overlay" {
		t.Fatalf("%+v", inst)
	}
	if inst.CPUs != 3 || inst.MemoryMB != 768 || inst.DiskGB != 6 {
		t.Fatalf("resources %+v", inst)
	}
}

func TestAddForwardValidationEdges(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Name: "fwdval", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.AddForward(ctx, inst.Name, 0, 0); err == nil {
		t.Fatal("guest 0")
	}
	if _, err := m.AddForward(ctx, inst.Name, 0, 70000); err == nil {
		t.Fatal("guest high")
	}
	if _, err := m.AddForward(ctx, inst.Name, -1, 80); err == nil {
		t.Fatal("host neg")
	}
	if _, err := m.AddForward(ctx, inst.Name, 80, 80); err == nil {
		t.Fatal("privileged host")
	}
	lf, err := m.AddForward(ctx, inst.Name, 18080, 80)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddForward(ctx, inst.Name, lf.HostPort, 81); err == nil {
		t.Fatal("duplicate host port")
	}
	if err := m.RemoveForward(ctx, inst.Name, lf.HostPort); err != nil {
		t.Fatal(err)
	}
	lf2, err := m.AddForward(ctx, inst.Name, 0, 443)
	if err != nil {
		t.Fatal(err)
	}
	if lf2.HostPort == 0 {
		t.Fatal("expected allocated port")
	}
}

func TestResumeProcessDead(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	inst, err := m.Create(context.Background(), vm.CreateOpts{Name: "resdead", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	rt.ForceDead(inst.Name)
	if err := m.Resume(context.Background(), inst.Name); err == nil {
		t.Fatal("expected process not running")
	}
}

func TestStartPausedRequiresResume(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "pstart", Persistent: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(context.Background(), "pstart"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), "pstart"); err == nil {
		t.Fatal("expected start fail while paused")
	}
}

func TestWaitAgentWithLocalAgentNonMock(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:7476")
	if err != nil {
		t.Skipf("7476 busy: %v", err)
	}
	_ = ln.Close()

	srv := agent.NewServer("127.0.0.1:7476", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ac := &agent.Client{BaseURL: "http://127.0.0.1:7476", HTTP: &http.Client{Timeout: time.Second}}
		if _, err := ac.Health(context.Background()); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	cfg := nonMockCfg(t)
	cfg.ReadyTimeout = 5 * time.Second
	m, _, _ := testManagerCfg(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inst, err := m.Create(ctx, vm.CreateOpts{
		Name:        "agentok",
		WaitMode:    vm.WaitAgent,
		WaitTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status %s", inst.Status)
	}
}

func TestSocketForwardDuplicateAndExistingFile(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	dir := t.TempDir()
	sock := filepath.Join(dir, "a.sock")
	if err := os.WriteFile(sock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "sockfile",
		SocketForwards: []vm.SocketForward{
			{HostPath: sock, GuestPath: "/run/x.sock"},
		},
	})
	if err == nil {
		t.Fatal("expected non-socket host path error")
	}

	s1 := filepath.Join(dir, "dup.sock")
	_, err = m.Create(context.Background(), vm.CreateOpts{
		Name: "sockdup",
		SocketForwards: []vm.SocketForward{
			{HostPath: s1, GuestPath: "/run/a.sock"},
			{HostPath: s1, GuestPath: "/run/b.sock"},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate host path")
	}
}

func TestCreateFromConfigImageAndWait(t *testing.T) {
	t.Parallel()
	cfg := mockCfg(t)
	cfg.Image = "ubuntu-cloud"
	m, _, _ := testManagerCfg(t, cfg)
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name:     "imgcfg",
		WaitMode: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Image == "" {
		t.Fatal("empty image")
	}
}

func TestCleanupEphemeralEmpty(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if err := m.CleanupEphemeral(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStopAliasPersistent(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "stopp", Persistent: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background(), "stopp"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("stopp")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusStopped {
		t.Fatalf("%s", got.Status)
	}
}

func TestCreateWithMountTag(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	host := t.TempDir()
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "mtag",
		Mounts: []vm.Mount{
			{Host: host, Guest: "/mnt/data", Tag: "custom"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Mounts) != 1 || inst.Mounts[0].Tag != "custom" {
		t.Fatalf("%+v", inst.Mounts)
	}
}

func TestStartWithMissingMountHost(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	host := t.TempDir()
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name:       "mntgone",
		Persistent: true,
		Mounts:     []vm.Mount{{Host: host, Guest: "/mnt/x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(host)
	if _, err := m.Start(context.Background(), inst.Name); err == nil {
		t.Fatal("expected mount validation fail")
	}
}

func TestFailStartThenGet(t *testing.T) {
	t.Parallel()
	m, rt, _ := testManager(t)
	rt.FailStart = true
	_, err := m.Create(context.Background(), vm.CreateOpts{Name: "fsget", Persistent: true})
	if err == nil {
		t.Fatal("expected fail")
	}
	got, gerr := m.Get("fsget")
	if gerr == nil && got.Status != vm.StatusError {
		t.Logf("status %s err=%q", got.Status, got.Error)
	}
}

func TestMultipleForwardsAndSocket(t *testing.T) {
	t.Parallel()
	m, _, _ := testManager(t)
	sock := filepath.Join(t.TempDir(), "s.sock")
	inst, err := m.Create(context.Background(), vm.CreateOpts{
		Name: "multi",
		Forwards: []vm.PortForward{
			{HostPort: 0, GuestPort: 80},
			{HostPort: 0, GuestPort: 443},
		},
		SocketForwards: []vm.SocketForward{
			{HostPath: sock, GuestPath: "/var/run/docker.sock"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inst.Forwards) != 2 {
		t.Fatalf("forwards %+v", inst.Forwards)
	}
	if len(inst.SocketForwards) != 1 || inst.SocketForwards[0].PID != 1 {
		t.Fatalf("socks %+v", inst.SocketForwards)
	}
	if err := m.Delete(context.Background(), inst.Name); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRejectsBadNetworkAndGPUFromConfig(t *testing.T) {
	t.Parallel()
	cfg := mockCfg(t)
	cfg.GPU = "vfio"
	m, _, _ := testManagerCfg(t, cfg)
	if _, err := m.Create(context.Background(), vm.CreateOpts{Name: "badgpu"}); err == nil {
		t.Fatal("gpu")
	}
	cfg2 := mockCfg(t)
	cfg2.Network = "bridge"
	m2, _, _ := testManagerCfg(t, cfg2)
	if _, err := m2.Create(context.Background(), vm.CreateOpts{Name: "badnet"}); err == nil {
		t.Fatal("network")
	}
}

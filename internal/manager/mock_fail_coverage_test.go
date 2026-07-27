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
	"github.com/cxdy/grain/internal/vm"
)

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

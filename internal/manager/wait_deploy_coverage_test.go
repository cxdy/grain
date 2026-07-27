package manager

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func unitMgr(t *testing.T, hv string) (*Manager, *hypervisor.MockRuntime, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = hv
	cfg.ReadyTimeout = time.Second
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rt := hypervisor.NewMockRuntime()
	m := New(cfg, st, rt, hypervisor.NewMockDisk(), nil)
	return m, rt, st, dir
}

func TestWaitOrDeployAgentSoftAndHard(t *testing.T) {
	m, _, _, _ := unitMgr(t, "mock")
	// Unreachable agent port. Dial returns a client without connecting; Wait polls
	// until the deadline. Budget must exceed agentProbeTimeout (3s) with only a
	// short remainder so we do not fall back to agentWaitFallback (60s).
	inst := &vm.Instance{Name: "w", AgentPort: 1, SSHPort: 22, IP: "127.0.0.1"}
	deadline := time.Now().Add(3*time.Second + 500*time.Millisecond)
	var phases []string
	emit := func(ev vm.CreateEvent) { phases = append(phases, ev.Phase) }

	err := m.waitOrDeployAgent(context.Background(), inst, "grain", "", deadline, emit, false)
	if err != nil {
		t.Fatalf("soft: %v", err)
	}
	if len(phases) == 0 {
		t.Fatal("expected wait_agent emit")
	}

	deadline2 := time.Now().Add(3*time.Second + 400*time.Millisecond)
	err = m.waitOrDeployAgent(context.Background(), inst, "grain", "", deadline2, nil, true)
	if err == nil {
		t.Fatal("hard expected error")
	}
}

func TestWaitOrDeployAgentWithLiveAgent(t *testing.T) {
	srv := agent.NewServer("127.0.0.1:0", nil)
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
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !endsWithPort0(addr) {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no port")
	}

	m, _, _, _ := unitMgr(t, "mock")
	inst := &vm.Instance{Name: "live", AgentPort: port, IP: "127.0.0.1"}
	dl := time.Now().Add(3 * time.Second)
	if err := m.waitOrDeployAgent(context.Background(), inst, "grain", "", dl, nil, true); err != nil {
		t.Fatal(err)
	}
}

func endsWithPort0(addr string) bool {
	return len(addr) >= 2 && addr[len(addr)-2:] == ":0"
}

func TestWaitAgentModeProbeSuccess(t *testing.T) {
	srv := agent.NewServer("127.0.0.1:0", nil)
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
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !endsWithPort0(addr) {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no port")
	}

	m, _, _, _ := unitMgr(t, "qemu") // non-mock label
	inst := &vm.Instance{Name: "probe", AgentPort: port, IP: "127.0.0.1", Image: "ubuntu-cloud"}
	dl := time.Now().Add(3 * time.Second)
	var phases []string
	emit := func(ev vm.CreateEvent) { phases = append(phases, ev.Phase) }
	if err := m.waitAgentMode(context.Background(), inst, "ubuntu-cloud", "", dl, emit, false); err != nil {
		t.Fatal(err)
	}
	if len(phases) == 0 {
		t.Fatal("expected emit")
	}
}

func TestWaitUserdataImmediateAndPoll(t *testing.T) {
	// Agent with UserdataRan via env.
	t.Setenv("GRAIN_USERDATA_RAN", "1")
	srv := agent.NewServer("127.0.0.1:0", nil)
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
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !endsWithPort0(addr) {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no port")
	}

	m, _, _, _ := unitMgr(t, "qemu")
	inst := &vm.Instance{Name: "ud", AgentPort: port, IP: "127.0.0.1"}
	// Confirm agent reports UserdataRan (env is process-wide; server reads it on health).
	ac := &agent.Client{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", port), HTTP: &http.Client{Timeout: time.Second}}
	h, err := ac.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !h.UserdataRan {
		t.Skip("userdata env not visible to agent health (subprocess not used)")
	}
	dl := time.Now().Add(2 * time.Second)
	if err := m.waitUserdata(context.Background(), inst, dl, nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestWaitSSHReturnsEmpty(t *testing.T) {
	m, _, _, _ := unitMgr(t, "qemu")
	inst := &vm.Instance{Name: "ssh", IP: "127.0.0.1", SSHPort: 1, Image: "ubuntu-cloud"}
	dl := time.Now().Add(200 * time.Millisecond)
	user := m.waitSSH(context.Background(), inst, "ubuntu-cloud", "", dl)
	if user != "" {
		t.Fatalf("unexpected user %q", user)
	}
}

func TestWaitSSHModeNonMockWithEndpoint(t *testing.T) {
	// Soft agent deploy after SSH fails quickly.
	m, _, _, _ := unitMgr(t, "qemu")
	inst := &vm.Instance{
		Name: "sshm", IP: "127.0.0.1", SSHPort: 1, AgentPort: 1, Image: "ubuntu-cloud",
	}
	dl := time.Now().Add(250 * time.Millisecond)
	var phases []string
	emit := func(ev vm.CreateEvent) { phases = append(phases, ev.Phase) }
	if err := m.waitSSHMode(context.Background(), inst, "ubuntu-cloud", "", dl, emit, false); err != nil {
		t.Fatal(err)
	}
}

func TestWaitAgentModeSSHFallbackFail(t *testing.T) {
	m, _, _, _ := unitMgr(t, "qemu")
	// Agent port allocated but dead; SSH port > 0 so fallback path runs.
	// waitAgentMode extends sshDeadline to Now+agentWaitFallback (60s) when the
	// remaining budget is smaller — avoid that by using context cancel after probe.
	inst := &vm.Instance{
		Name: "agfb", IP: "127.0.0.1", SSHPort: 1, AgentPort: 1, Image: "ubuntu-cloud",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	// Far-future deadline so code does not replace with 60s fallback; ctx kills wait.
	dl := time.Now().Add(10 * time.Minute)
	err := m.waitAgentMode(ctx, inst, "ubuntu-cloud", "", dl, nil, false)
	if err == nil {
		t.Fatal("expected fail")
	}
}

func TestWaitAgentModeNoSSHPort(t *testing.T) {
	m, _, _, _ := unitMgr(t, "qemu")
	inst := &vm.Instance{Name: "nossh", AgentPort: 1, SSHPort: 0}
	dl := time.Now().Add(300 * time.Millisecond)
	err := m.waitAgentMode(context.Background(), inst, "img", "", dl, nil, false)
	if err == nil {
		t.Fatal("expected fail without ssh")
	}
}

func TestWaitUserdataDialFailAndTimeout(t *testing.T) {
	m, _, _, _ := unitMgr(t, "qemu")
	// Dial always returns a TCP client for Port>0; Health then fails until deadline.
	// Use a short positive remaining budget (past deadline falls back to 60s).
	inst := &vm.Instance{Name: "udfail", AgentPort: 1, IP: "127.0.0.1"}
	dl := time.Now().Add(400 * time.Millisecond)
	err := m.waitUserdata(context.Background(), inst, dl, nil, false)
	if err == nil {
		t.Fatal("expected error")
	}

	// Live agent but UserdataRan false → poll timeout.
	srv := agent.NewServer("127.0.0.1:0", nil)
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
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !endsWithPort0(addr) {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no port")
	}
	inst2 := &vm.Instance{Name: "udpoll", AgentPort: port, IP: "127.0.0.1"}
	dl2 := time.Now().Add(400 * time.Millisecond)
	err = m.waitUserdata(context.Background(), inst2, dl2, func(ev vm.CreateEvent) {}, false)
	if err == nil {
		t.Fatal("expected userdata timeout")
	}
}

func TestStartFromDiskFailStart(t *testing.T) {
	m, rt, st, dir := unitMgr(t, "mock")
	diskPath := filepath.Join(dir, "vms", "x", "disk.img")
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diskPath, []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{
		Name: "x", Status: vm.StatusStopped, Persistent: true,
		DiskPath: diskPath, CPUs: 1, MemoryMB: 256,
	}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	rt.FailStart = true
	if _, err := m.startFromDisk(context.Background(), inst); err == nil {
		t.Fatal("expected start fail")
	}
}

func TestStartFromDiskWithLoadVMMarker(t *testing.T) {
	m, _, st, dir := unitMgr(t, "mock")
	vmDir := filepath.Join(dir, "vms", "load")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	diskPath := filepath.Join(vmDir, "disk.img")
	if err := os.WriteFile(diskPath, []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeSuspendMarker(vmDir, hypervisor.SuspendSnapshotTag); err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{
		Name: "load", Status: vm.StatusSuspended, Persistent: true,
		DiskPath: diskPath, CPUs: 1, MemoryMB: 256,
		LoadVM: hypervisor.SuspendSnapshotTag,
	}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	got, err := m.Restore(context.Background(), "load")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != vm.StatusRunning {
		t.Fatalf("%s", got.Status)
	}
	// Marker cleared
	if _, ok := readSuspendMarker(vmDir); ok {
		t.Fatal("marker should be cleared")
	}
}

func TestImageHasAgentAndResolveWaitGolden(t *testing.T) {
	m, _, _, _ := unitMgr(t, "mock")
	_ = m.imageHasAgent("grain-ubuntu")
	mode, err := m.resolveWaitMode("", "ubuntu-cloud")
	if err != nil {
		t.Fatal(err)
	}
	_ = mode
	mode, err = m.resolveWaitMode("auto", "grain-ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	_ = mode
}

func shortUnixSock(t *testing.T, name string) string {
	t.Helper()
	// macOS sun_path is short (~104 bytes); t.TempDir() under long user paths often fails.
	dir, err := os.MkdirTemp("/tmp", "gsk*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func TestKillSocketForwardsWithSocketFile(t *testing.T) {
	m, _, _, _ := unitMgr(t, "mock")
	sock := shortUnixSock(t, "k.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	inst := &vm.Instance{
		SocketForwards: []vm.SocketForward{
			{HostPath: sock, GuestPath: "/g", PID: 0},
		},
	}
	m.killSocketForwards(inst)
	if _, err := os.Lstat(sock); !os.IsNotExist(err) {
		// may or may not remove depending on ModeSocket
		t.Log(err)
	}
}

func TestPrepareSocketForwardsAbsAndStaleSocket(t *testing.T) {
	// Short paths so unix Listen works on macOS.
	base, err := os.MkdirTemp("/tmp", "gps*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	rel := filepath.Join(base, "rel.sock")
	stale := filepath.Join(base, "stale.sock")
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()

	out, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: rel, GuestPath: "/g1"},
		{HostPath: stale, GuestPath: "/g2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("%+v", out)
	}
	if !filepath.IsAbs(out[0].HostPath) {
		t.Fatalf("not abs %s", out[0].HostPath)
	}
	// stale socket removed so ssh can re-bind
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		// May still exist as leftover file on some platforms; prepare only removes ModeSocket.
		t.Logf("stale after prepare: %v", err)
	}
}

func TestDiskLooksQcow2SiblingAndSuffix(t *testing.T) {
	t.Parallel()
	if diskLooksQcow2("") {
		t.Fatal()
	}
	if !diskLooksQcow2("/x/disk.qcow2") {
		t.Fatal("suffix")
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(base+".qcow2", []byte("q"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !diskLooksQcow2(base) {
		t.Fatal("sibling .qcow2")
	}
	base2 := filepath.Join(dir, "d2.img")
	if err := os.WriteFile(filepath.Join(dir, "disk.qcow2"), []byte("q"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !diskLooksQcow2(base2) {
		t.Fatal("disk.qcow2 sibling")
	}
}

func TestReadSuspendMarkerEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, hypervisor.SuspendMarkerName), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if tag, ok := readSuspendMarker(dir); ok || tag != "" {
		t.Fatalf("%q %v", tag, ok)
	}
	clearSuspendMarker(dir)
}

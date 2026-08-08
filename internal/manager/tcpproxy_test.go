package manager

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func TestFCCreateTimePublishSpecs(t *testing.T) {
	t.Parallel()
	fwds := []vm.PortForward{
		{HostPort: 18080, GuestPort: 8080},
		{HostPort: 0, GuestPort: 90},                  // skipped
		{HostPort: 5353, GuestPort: 53, Proto: "udp"}, // skipped (tcp proxy only)
		{HostPort: 19000, GuestPort: 9000},
	}
	live := []vm.LiveForward{{HostPort: 19000, GuestPort: 9000, PID: 1}}
	got := FCCreateTimePublishSpecs(fwds, live, 0, 0)
	if len(got) != 1 || got[0].HostPort != 18080 || got[0].GuestPort != 8080 {
		t.Fatalf("got %+v", got)
	}
	// All covered → empty.
	if n := FCCreateTimePublishSpecs(fwds[:1], []vm.LiveForward{{HostPort: 18080, GuestPort: 8080}}, 0, 0); len(n) != 0 {
		t.Fatalf("expected empty, got %+v", n)
	}
	// SSH + agent host ports included when allocated.
	got2 := FCCreateTimePublishSpecs(nil, nil, 2200, 17475)
	if len(got2) != 2 {
		t.Fatalf("ssh+agent: %+v", got2)
	}
	if got2[0].HostPort != 2200 || got2[0].GuestPort != 22 {
		t.Fatalf("ssh rule %+v", got2[0])
	}
	if got2[1].HostPort != 17475 || got2[1].GuestPort != 7475 {
		t.Fatalf("agent rule %+v", got2[1])
	}
	// SSH already live → only agent.
	got3 := FCCreateTimePublishSpecs(nil, []vm.LiveForward{{HostPort: 2200, GuestPort: 22}}, 2200, 17475)
	if len(got3) != 1 || got3[0].HostPort != 17475 {
		t.Fatalf("covered ssh: %+v", got3)
	}
}

func TestStartTCPProxyRoundTrip(t *testing.T) {
	// Backend that echoes one line.
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	_, backendPortStr, _ := net.SplitHostPort(backend.Addr().String())
	backendPort, _ := strconv.Atoi(backendPortStr)

	go func() {
		c, err := backend.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		_, _ = c.Write(append([]byte("echo:"), buf[:n]...))
	}()

	// Free host port for proxy.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, hostPortStr, _ := net.SplitHostPort(l.Addr().String())
	hostPort, _ := strconv.Atoi(hostPortStr)
	_ = l.Close()

	pid, err := startTCPProxy(hostPort, "127.0.0.1", backendPort)
	if err != nil {
		t.Skipf("proxy unavailable in this env: %v", err)
	}
	defer func() { _ = killPID(pid) }()

	deadline := time.Now().Add(3 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+hostPortStr, 200*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("hi"))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil && len(got) == 0 {
		t.Fatal(err)
	}
	if string(got) != "echo:hi" {
		t.Fatalf("got %q", got)
	}
}

func TestStartTCPProxyInvalidEndpoints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		host int
		ip   string
		g    int
	}{
		{0, "127.0.0.1", 80},
		{-1, "127.0.0.1", 80},
		{18080, "", 80},
		{18080, "127.0.0.1", 0},
		{18080, "127.0.0.1", -1},
	}
	for _, tc := range cases {
		if _, err := startTCPProxy(tc.host, tc.ip, tc.g); err == nil {
			t.Fatalf("expected error for host=%d ip=%q guest=%d", tc.host, tc.ip, tc.g)
		}
	}
}

func TestStartDetachedProxyDied(t *testing.T) {
	t.Parallel()
	// Process exits immediately → Signal(0) fails after sleep.
	// Absolute path so a concurrent PATH-mutating test cannot break LookPath.
	bin, err := exec.LookPath("false")
	if err != nil {
		t.Skip("false not on PATH")
	}
	pid, err := startDetached(bin)
	if err == nil {
		t.Fatalf("expected proxy died, pid=%d", pid)
	}
	if !strings.Contains(err.Error(), "proxy died") {
		t.Fatalf("err %v", err)
	}
}

func TestStartDetachedBadBinary(t *testing.T) {
	t.Parallel()
	if _, err := startDetached(filepath.Join(t.TempDir(), "no-such-bin")); err == nil {
		t.Fatal("expected start error")
	}
}

func TestStartTCPProxyNoTools(t *testing.T) {
	// Empty PATH → neither socat nor python3.
	t.Setenv("PATH", t.TempDir())
	if _, err := startTCPProxy(19999, "127.0.0.1", 80); err == nil || !strings.Contains(err.Error(), "socat or python3") {
		t.Fatalf("want missing tools error: %v", err)
	}
}

func TestStartTCPProxyPythonFallback(t *testing.T) {
	// Force socat missing so python3 path is exercised when available.
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	origPath := os.Getenv("PATH")
	// PATH without socat: only dirs that do not contain socat, but keep python3.
	// Use a temp dir with a symlink to python3 only.
	dir := t.TempDir()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(py, filepath.Join(dir, "python3")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	_, bpStr, _ := net.SplitHostPort(backend.Addr().String())
	bp, _ := strconv.Atoi(bpStr)
	go func() {
		c, err := backend.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 32)
		n, _ := c.Read(buf)
		_, _ = c.Write(buf[:n])
	}()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, hpStr, _ := net.SplitHostPort(l.Addr().String())
	hp, _ := strconv.Atoi(hpStr)
	_ = l.Close()

	pid, err := startTCPProxy(hp, "127.0.0.1", bp)
	if err != nil {
		t.Fatalf("python proxy: %v (PATH was %s; orig %s)", err, dir, origPath)
	}
	defer func() { _ = killPID(pid) }()

	var conn net.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+hpStr, 200*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("py"))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, _ := io.ReadAll(conn)
	if string(got) != "py" {
		t.Fatalf("got %q", got)
	}
}

func TestStartFCCreateTimeProxiesBranches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)

	// Non-firecracker early return.
	if err := m.startFCCreateTimeProxies(&vm.Instance{Name: "x", IP: "10.0.0.2"}); err != nil {
		t.Fatal(err)
	}
	if err := m.startFCCreateTimeProxies(nil); err != nil {
		t.Fatal(err)
	}

	m.cfg.Hypervisor = "firecracker"
	// No guest IP.
	if err := m.startFCCreateTimeProxies(&vm.Instance{Name: "x"}); err == nil || !strings.Contains(err.Error(), "no guest IP") {
		t.Fatalf("empty ip: %v", err)
	}
	if err := m.startFCCreateTimeProxies(&vm.Instance{Name: "x", IP: "127.0.0.1"}); err == nil {
		t.Fatal("loopback ip")
	}
	// Empty specs.
	inst := &vm.Instance{Name: "fc1", IP: "10.0.0.5"}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	if err := m.startFCCreateTimeProxies(inst); err != nil {
		t.Fatal(err)
	}

	// With publish specs: may succeed (socat/python) or record first error.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, hpStr, _ := net.SplitHostPort(l.Addr().String())
	hp, _ := strconv.Atoi(hpStr)
	_ = l.Close()

	inst.Forwards = []vm.PortForward{{HostPort: hp, GuestPort: 80}}
	// No backend; proxy process may still start (python/socat listen).
	err = m.startFCCreateTimeProxies(inst)
	// Either nil (proxy started) or error (proxy died / missing tools).
	// Both exercise the loop; clean up any PIDs.
	for _, lf := range inst.LiveForwards {
		_ = killPID(lf.PID)
	}
	_ = err
}

func TestConfigureFCGuestNetEarly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	ctx := context.Background()

	if err := m.configureFCGuestNet(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := m.configureFCGuestNet(ctx, &vm.Instance{DiskPath: "/x"}); err != nil {
		t.Fatal(err)
	}
	m.cfg.Hypervisor = "firecracker"
	if err := m.configureFCGuestNet(ctx, &vm.Instance{Name: "n"}); err != nil {
		t.Fatal(err)
	}
	// No FC net state file → silent nil.
	vmDir := filepath.Join(dir, "fcvm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{Name: "fc", DiskPath: filepath.Join(vmDir, "disk.raw")}
	if err := m.configureFCGuestNet(ctx, inst); err != nil {
		t.Fatal(err)
	}
}

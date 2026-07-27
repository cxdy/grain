package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func TestParseGuestArchAll(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"", "", true},
		{"host", "", true},
		{"native", "", true},
		{"auto", "", true},
		{"arm64", "arm64", true},
		{"AARCH64", "arm64", true},
		{"amd64", "amd64", true},
		{"x86_64", "amd64", true},
		{"x86-64", "amd64", true},
		{"x64", "amd64", true},
		{"  amd64  ", "amd64", true},
		{"riscv", "", false},
		{"ppc64", "", false},
	}
	for _, tc := range cases {
		got, err := parseGuestArch(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("%q: got %q %v want %q", tc.in, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("%q: expected error", tc.in)
		}
	}
}

func TestActiveStatus(t *testing.T) {
	t.Parallel()
	for _, s := range []vm.Status{vm.StatusRunning, vm.StatusCreating, vm.StatusPaused} {
		if !activeStatus(s) {
			t.Fatalf("%s should be active", s)
		}
	}
	for _, s := range []vm.Status{vm.StatusStopped, vm.StatusError, vm.StatusSuspended, ""} {
		if activeStatus(s) {
			t.Fatalf("%s should not be active", s)
		}
	}
}

func TestMountSpecs(t *testing.T) {
	t.Parallel()
	if mountSpecs(nil, "9p") != nil {
		t.Fatal("nil mounts")
	}
	out := mountSpecs([]vm.Mount{{Tag: "t0", Guest: "/g"}}, "")
	if len(out) != 1 || out[0].Driver != "9p" || out[0].Tag != "t0" || out[0].Guest != "/g" {
		t.Fatalf("%+v", out)
	}
	out = mountSpecs([]vm.Mount{{Tag: "v", Guest: "/w"}}, "virtiofs")
	if out[0].Driver != "virtiofs" {
		t.Fatalf("%+v", out)
	}
}

func TestPrepareMounts(t *testing.T) {
	t.Parallel()
	if out, err := prepareMounts(nil); err != nil || out != nil {
		t.Fatalf("nil: %v %v", out, err)
	}
	dir := t.TempDir()
	out, err := prepareMounts([]vm.Mount{
		{Host: dir, Guest: "/mnt/a"},
		{Host: dir, Guest: "/mnt/b", Tag: "custom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Tag != "grain0" || out[1].Tag != "custom" {
		t.Fatalf("tags %+v", out)
	}
	if !filepath.IsAbs(out[0].Host) {
		t.Fatalf("host not abs: %s", out[0].Host)
	}
	// empty host
	if _, err := prepareMounts([]vm.Mount{{Host: "", Guest: "/x"}}); err == nil {
		t.Fatal("empty host")
	}
	// empty guest
	if _, err := prepareMounts([]vm.Mount{{Host: dir, Guest: ""}}); err == nil {
		t.Fatal("empty guest")
	}
	// relative guest
	if _, err := prepareMounts([]vm.Mount{{Host: dir, Guest: "rel"}}); err == nil {
		t.Fatal("relative guest")
	}
	// missing host
	if _, err := prepareMounts([]vm.Mount{{Host: filepath.Join(dir, "nope"), Guest: "/x"}}); err == nil {
		t.Fatal("missing host")
	}
	// file not dir
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMounts([]vm.Mount{{Host: f, Guest: "/x"}}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file host: %v", err)
	}
}

func TestValidateStoredMounts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := validateStoredMounts(nil); err != nil {
		t.Fatal(err)
	}
	if err := validateStoredMounts([]vm.Mount{{Host: dir, Tag: "g0"}}); err != nil {
		t.Fatal(err)
	}
	if err := validateStoredMounts([]vm.Mount{{Host: "", Tag: "g0"}}); err == nil {
		t.Fatal("incomplete")
	}
	if err := validateStoredMounts([]vm.Mount{{Host: dir, Tag: ""}}); err == nil {
		t.Fatal("empty tag")
	}
	if err := validateStoredMounts([]vm.Mount{{Host: filepath.Join(dir, "missing"), Tag: "g0"}}); err == nil {
		t.Fatal("missing")
	}
	f := filepath.Join(dir, "file")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	if err := validateStoredMounts([]vm.Mount{{Host: f, Tag: "g0"}}); err == nil {
		t.Fatal("file")
	}
}

func TestPrepareSocketForwards(t *testing.T) {
	t.Parallel()
	if out, err := prepareSocketForwards(nil); err != nil || out != nil {
		t.Fatalf("nil: %v %v", out, err)
	}
	dir := t.TempDir()
	host := filepath.Join(dir, "d.sock")
	out, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: host, GuestPath: "/var/run/docker.sock"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].HostPath != host {
		t.Fatalf("%+v", out)
	}
	// relative host becomes abs
	rel := filepath.Join(".", "rel.sock")
	out, err = prepareSocketForwards([]vm.SocketForward{
		{HostPath: rel, GuestPath: "/g"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(out[0].HostPath) {
		t.Fatalf("want abs: %s", out[0].HostPath)
	}
	// empty host
	if _, err := prepareSocketForwards([]vm.SocketForward{{HostPath: "", GuestPath: "/g"}}); err == nil {
		t.Fatal("empty host")
	}
	if _, err := prepareSocketForwards([]vm.SocketForward{{HostPath: ".", GuestPath: "/g"}}); err == nil {
		t.Fatal("dot host")
	}
	// guest not absolute
	if _, err := prepareSocketForwards([]vm.SocketForward{{HostPath: host + "2", GuestPath: "rel"}}); err == nil {
		t.Fatal("relative guest")
	}
	// duplicate host
	if _, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: host, GuestPath: "/a"},
		{HostPath: host, GuestPath: "/b"},
	}); err == nil {
		t.Fatal("duplicate")
	}
	// existing non-socket file
	f := filepath.Join(dir, "notsock")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	if _, err := prepareSocketForwards([]vm.SocketForward{{HostPath: f, GuestPath: "/g"}}); err == nil {
		t.Fatal("existing file")
	}
	// existing socket is removed
	// (os.ModeSocket may not be settable portably via Create; skip if platform cannot)
}

func TestCopyAndPrepareForwards(t *testing.T) {
	t.Parallel()
	if out, err := copyAndPrepareForwards(nil); err != nil || out != nil {
		t.Fatalf("nil: %v %v", out, err)
	}
	out, err := copyAndPrepareForwards([]vm.PortForward{
		{HostPort: 0, GuestPort: 80},
		{HostPort: 18080, GuestPort: 443, Proto: "tcp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].HostPort < 1024 {
		t.Fatalf("auto port %d", out[0].HostPort)
	}
	if out[1].HostPort != 18080 {
		t.Fatalf("%+v", out)
	}
	// privileged
	if _, err := copyAndPrepareForwards([]vm.PortForward{{HostPort: 80, GuestPort: 80}}); err == nil {
		t.Fatal("privileged")
	}
	// bad guest
	if _, err := copyAndPrepareForwards([]vm.PortForward{{HostPort: 8080, GuestPort: 0}}); err == nil {
		t.Fatal("bad guest")
	}
}

func TestSuspendMarkers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeSuspendMarker(dir, "grain-suspend"); err != nil {
		t.Fatal(err)
	}
	tag, ok := readSuspendMarker(dir)
	if !ok || tag != "grain-suspend" {
		t.Fatalf("%q %v", tag, ok)
	}
	// empty marker
	_ = os.WriteFile(filepath.Join(dir, hypervisor.SuspendMarkerName), []byte("  \n"), 0o644)
	if _, ok := readSuspendMarker(dir); ok {
		t.Fatal("empty should be false")
	}
	// missing
	missing := filepath.Join(dir, "sub")
	if _, ok := readSuspendMarker(missing); ok {
		t.Fatal("missing")
	}
	clearSuspendMarker(dir)
	if _, ok := readSuspendMarker(dir); ok {
		t.Fatal("cleared")
	}
	// nested write creates dir
	nested := filepath.Join(dir, "nested-vm")
	if err := writeSuspendMarker(nested, "t"); err != nil {
		t.Fatal(err)
	}
}

func TestDiskLooksQcow2(t *testing.T) {
	t.Parallel()
	if diskLooksQcow2("") {
		t.Fatal("empty")
	}
	if !diskLooksQcow2("/tmp/disk.qcow2") {
		t.Fatal("suffix")
	}
	dir := t.TempDir()
	raw := filepath.Join(dir, "disk.img")
	_ = os.WriteFile(raw, []byte("raw"), 0o644)
	if diskLooksQcow2(raw) {
		t.Fatal("raw alone")
	}
	// sibling .qcow2
	_ = os.WriteFile(raw+".qcow2", []byte("qc"), 0o644)
	if !diskLooksQcow2(raw) {
		t.Fatal("sibling .qcow2")
	}
	// disk.qcow2 in same dir
	dir2 := t.TempDir()
	raw2 := filepath.Join(dir2, "disk.img")
	_ = os.WriteFile(raw2, []byte("r"), 0o644)
	_ = os.WriteFile(filepath.Join(dir2, "disk.qcow2"), []byte("q"), 0o644)
	if !diskLooksQcow2(raw2) {
		t.Fatal("disk.qcow2 sibling")
	}
}

func TestKillPID(t *testing.T) {
	t.Parallel()
	if err := killPID(0); err != nil {
		t.Fatal(err)
	}
	if err := killPID(-1); err != nil {
		t.Fatal(err)
	}
	// Unlikely PID — should not panic.
	_ = killPID(1<<28 - 3)
}

func TestAgentTarget(t *testing.T) {
	t.Parallel()
	if agentTarget(nil).HasEndpoint() {
		t.Fatal("nil")
	}
	if agentTarget(&vm.Instance{}).HasEndpoint() {
		t.Fatal("empty")
	}
	tgt := agentTarget(&vm.Instance{AgentPort: 9, AgentCID: 0})
	if !tgt.HasEndpoint() || tgt.Port != 9 {
		t.Fatalf("%+v", tgt)
	}
	tgt = agentTarget(&vm.Instance{AgentCID: 3})
	if !tgt.HasEndpoint() {
		t.Fatal("cid")
	}
}

func TestEmitCreate(t *testing.T) {
	t.Parallel()
	// nil OnEvent should not panic
	emitCreate(vm.CreateOpts{}, vm.CreateEvent{Phase: "x"})
	var got string
	emitCreate(vm.CreateOpts{OnEvent: func(ev vm.CreateEvent) { got = ev.Phase }}, vm.CreateEvent{Phase: "y"})
	if got != "y" {
		t.Fatalf("%q", got)
	}
}

func TestNewNilLog(t *testing.T) {
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
	if m == nil || m.log == nil {
		t.Fatal("nil manager or log")
	}
}

func TestFailHelper(t *testing.T) {
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
	inst := &vm.Instance{Name: "f1", Status: vm.StatusCreating}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	var phase string
	_, err = m.fail(inst, context.Canceled, vm.CreateOpts{
		OnEvent: func(ev vm.CreateEvent) { phase = ev.Phase },
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if phase != vm.PhaseError {
		t.Fatalf("phase %q", phase)
	}
	got, _ := st.Get("f1")
	if got.Status != vm.StatusError || got.Error == "" {
		t.Fatalf("%+v", got)
	}
	// fail without opts
	inst2 := &vm.Instance{Name: "f2", Status: vm.StatusCreating}
	_ = st.Put(inst2)
	_, err = m.fail(inst2, context.DeadlineExceeded)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveImageID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Image = "ubuntu-cloud"
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if g := m.resolveImageID("explicit"); g != "explicit" {
		t.Fatalf("%q", g)
	}
	if g := m.resolveImageID(""); g != "ubuntu-cloud" {
		t.Fatalf("cfg default %q", g)
	}
	if g := m.resolveImageID("auto"); g == "" {
		t.Fatal("auto empty")
	}
	cfg.Image = "auto"
	m = New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if g := m.resolveImageID(""); g == "" {
		t.Fatal("auto config")
	}
	cfg.Image = ""
	m = New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if g := m.resolveImageID(""); g == "" {
		t.Fatal("empty → default")
	}
}

func TestResolveWaitMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	mode, err := m.resolveWaitMode("ssh", "ubuntu-cloud")
	if err != nil || mode != vm.WaitSSH {
		t.Fatalf("%q %v", mode, err)
	}
	mode, err = m.resolveWaitMode("agent", "x")
	if err != nil || mode != vm.WaitAgent {
		t.Fatalf("%q %v", mode, err)
	}
	mode, err = m.resolveWaitMode("userdata", "x")
	if err != nil || mode != vm.WaitUserdata {
		t.Fatalf("%q %v", mode, err)
	}
	if _, err := m.resolveWaitMode("nope", "x"); err == nil {
		t.Fatal("invalid")
	}
	// auto without golden → ssh
	mode, err = m.resolveWaitMode("auto", "ubuntu-cloud")
	if err != nil || mode != vm.WaitSSH {
		// may be agent if golden present; either valid
		if mode != vm.WaitSSH && mode != vm.WaitAgent {
			t.Fatalf("%q %v", mode, err)
		}
	}
}

func TestResolveSSHUser(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.SSHUser = "custom"
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if g := m.resolveSSHUser("ubuntu-cloud"); g != "custom" {
		t.Fatalf("%q", g)
	}
	cfg.SSHUser = ""
	m = New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if g := m.resolveSSHUser("ubuntu-cloud"); g == "" {
		t.Fatal("empty fallback")
	}
	cfg.SSHUser = "alpine"
	m = New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	// alpine triggers catalog lookup for image SSH user
	_ = m.resolveSSHUser("ubuntu-cloud")
	_ = m.resolveSSHUser("no-such-image-zzz")
}

func TestImageHasAgent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if m.imageHasAgent("") {
		t.Fatal("empty")
	}
	// grain-ubuntu may or may not be local; just exercise call
	_ = m.imageHasAgent("grain-ubuntu")
	_ = m.imageHasAgent("ubuntu-cloud")
}

func TestKillLiveAndSocketForwards(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	inst := &vm.Instance{
		Name: "k",
		LiveForwards: []vm.LiveForward{
			{HostPort: 1, GuestPort: 2, PID: 0},
			{HostPort: 3, GuestPort: 4, PID: -1},
		},
		SocketForwards: []vm.SocketForward{
			{HostPath: filepath.Join(dir, "a.sock"), GuestPath: "/g", PID: 0},
		},
	}
	m.killLiveForwards(inst)
	if inst.LiveForwards != nil {
		t.Fatalf("live %v", inst.LiveForwards)
	}
	m.killSocketForwards(inst)
	if inst.SocketForwards[0].PID != 0 {
		t.Fatalf("pid %d", inst.SocketForwards[0].PID)
	}
}

func TestStartSocketForwardsMock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	inst := &vm.Instance{Name: "s"}
	if err := m.startSocketForwards(inst); err != nil {
		t.Fatal(err)
	}
	inst.SocketForwards = []vm.SocketForward{
		{HostPath: filepath.Join(dir, "x.sock"), GuestPath: "/g"},
	}
	if err := m.startSocketForwards(inst); err != nil {
		t.Fatal(err)
	}
	if inst.SocketForwards[0].PID != 1 {
		t.Fatalf("pid %d", inst.SocketForwards[0].PID)
	}
}

func TestWaitReadyMockBranches(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	inst := &vm.Instance{Name: "w", SSHPort: 22}
	deadline := time.Now().Add(time.Second)
	var phases []string
	emit := func(ev vm.CreateEvent) { phases = append(phases, ev.Phase) }
	if err := m.waitReady(context.Background(), inst, "ubuntu-cloud", "", vm.WaitSSH, deadline, emit); err != nil {
		t.Fatal(err)
	}
	if err := m.waitReady(context.Background(), inst, "ubuntu-cloud", "", vm.WaitAgent, deadline, emit); err != nil {
		t.Fatal(err)
	}
	if err := m.waitReady(context.Background(), inst, "ubuntu-cloud", "", vm.WaitUserdata, deadline, emit); err != nil {
		t.Fatal(err)
	}
	// nil emit
	if err := m.waitSSHMode(context.Background(), inst, "img", "", deadline, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := m.waitAgentMode(context.Background(), inst, "img", "", deadline, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := m.waitUserdata(context.Background(), inst, deadline, nil, true); err != nil {
		t.Fatal(err)
	}
	// isMock false + no agent endpoint
	if err := m.waitAgentMode(context.Background(), &vm.Instance{Name: "n"}, "img", "", deadline, emit, false); err == nil {
		t.Fatal("expected no endpoint")
	}
	if err := m.waitUserdata(context.Background(), &vm.Instance{Name: "n"}, deadline, emit, false); err == nil {
		t.Fatal("expected no endpoint")
	}
	// waitSSHMode mock with zero ssh port
	if err := m.waitSSHMode(context.Background(), &vm.Instance{Name: "z", SSHPort: 0}, "img", "", deadline, emit, true); err != nil {
		t.Fatal(err)
	}
	// non-mock waitSSHMode with SSHPort 0 skips wait
	if err := m.waitSSHMode(context.Background(), &vm.Instance{Name: "z2", SSHPort: 0}, "img", "", deadline, emit, false); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeWaitModeUnit(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "auto", "AUTO", " ssh ", "Agent", "userdata"} {
		if _, err := NormalizeWaitMode(in); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
	}
	if _, err := NormalizeWaitMode("wat"); err == nil {
		t.Fatal("invalid")
	}
}

func TestCheckResourceCapsEdges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, _ := store.New(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.MaxCPUsPerVM = 2
	cfg.MaxMemoryMBPerVM = 1024
	cfg.MaxVMs = 1
	cfg.MaxCPUsTotal = 4
	cfg.MaxMemoryMBTotal = 2048
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if err := m.checkResourceCaps(4, 512, ""); err == nil {
		t.Fatal("cpus per vm")
	}
	if err := m.checkResourceCaps(1, 2048, ""); err == nil {
		t.Fatal("mem per vm")
	}
	// put a running VM
	_ = st.Put(&vm.Instance{Name: "a", Status: vm.StatusRunning, CPUs: 2, MemoryMB: 1024})
	if err := m.checkResourceCaps(1, 512, ""); err == nil {
		t.Fatal("max vms")
	}
	// excludeName skips self
	if err := m.checkResourceCaps(1, 512, "a"); err != nil {
		t.Fatalf("exclude: %v", err)
	}
	// stopped does not count
	_ = st.Put(&vm.Instance{Name: "a", Status: vm.StatusStopped, CPUs: 2, MemoryMB: 1024})
	if err := m.checkResourceCaps(1, 512, ""); err != nil {
		t.Fatalf("stopped: %v", err)
	}
	// total caps
	_ = st.Put(&vm.Instance{Name: "a", Status: vm.StatusRunning, CPUs: 3, MemoryMB: 512})
	cfg.MaxVMs = 0
	m = New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	if err := m.checkResourceCaps(2, 512, ""); err == nil {
		t.Fatal("total cpus")
	}
	_ = st.Put(&vm.Instance{Name: "a", Status: vm.StatusRunning, CPUs: 1, MemoryMB: 1800})
	if err := m.checkResourceCaps(1, 512, ""); err == nil {
		t.Fatal("total mem")
	}
}

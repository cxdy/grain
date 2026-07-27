package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/cloudinit"
	"github.com/cxdy/grain/internal/vm"
)

func TestNormalizeWaitModeInternal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
		err      bool
	}{
		{"", "", false},
		{"auto", "", false},
		{"ssh", vm.WaitSSH, false},
		{"agent", vm.WaitAgent, false},
		{"userdata", vm.WaitUserdata, false},
		{"SSH", vm.WaitSSH, false},
		{"nope", "", true},
		{"true", "", true},
	}
	for _, tc := range cases {
		got, err := NormalizeWaitMode(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("%q: want err", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%q: got %q %v want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestParseGuestArchCoverage(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "host", "native", "auto", "arm64", "aarch64", "amd64", "x86_64", "x64"} {
		if _, err := parseGuestArch(in); err != nil {
			t.Fatalf("%q: %v", in, err)
		}
	}
	if _, err := parseGuestArch("riscv"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDiskLooksQcow2AndSuspendMarkerCoverage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if diskLooksQcow2("") {
		t.Fatal("empty")
	}
	if diskLooksQcow2(filepath.Join(dir, "nope")) {
		t.Fatal("missing")
	}
	p := filepath.Join(dir, "d.qcow2")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !diskLooksQcow2(p) {
		t.Fatal("suffix .qcow2")
	}
	raw := filepath.Join(dir, "d.raw")
	if err := os.WriteFile(raw, []byte("notqcow"), 0o644); err != nil {
		t.Fatal(err)
	}
	if diskLooksQcow2(raw) {
		t.Fatal("raw")
	}
	// sibling disk.qcow2
	if err := os.WriteFile(filepath.Join(dir, "disk.qcow2"), []byte("q"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !diskLooksQcow2(raw) {
		t.Fatal("sibling")
	}

	vmDir := filepath.Join(dir, "vm")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSuspendMarker(vmDir, "snap1"); err != nil {
		t.Fatal(err)
	}
	tag, ok := readSuspendMarker(vmDir)
	if !ok || tag != "snap1" {
		t.Fatalf("%q %v", tag, ok)
	}
	clearSuspendMarker(vmDir)
	if _, ok := readSuspendMarker(vmDir); ok {
		t.Fatal("cleared")
	}
	if err := os.WriteFile(filepath.Join(vmDir, "suspend.tag"), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readSuspendMarker(vmDir); ok {
		t.Fatal("empty tag")
	}
}

func TestPrepareSocketForwardsAndMountsCoverage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hostSock := filepath.Join(dir, "d.sock")
	if _, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: hostSock, GuestPath: "rel"},
	}); err == nil {
		t.Fatal("relative guest")
	}
	if _, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: "", GuestPath: "/var/run/docker.sock"},
	}); err == nil {
		t.Fatal("empty host")
	}
	got, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: hostSock, GuestPath: "/var/run/docker.sock"},
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	// duplicate
	if _, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: hostSock, GuestPath: "/a"},
		{HostPath: hostSock, GuestPath: "/b"},
	}); err == nil {
		t.Fatal("dup")
	}
	// non-socket existing file
	reg := filepath.Join(dir, "file")
	if err := os.WriteFile(reg, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareSocketForwards([]vm.SocketForward{
		{HostPath: reg, GuestPath: "/g"},
	}); err == nil {
		t.Fatal("nonsock")
	}

	if _, err := prepareMounts(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareMounts([]vm.Mount{{Host: "", Guest: "/m"}}); err == nil {
		t.Fatal("empty host mount")
	}
	if _, err := prepareMounts([]vm.Mount{{Host: dir, Guest: "rel"}}); err == nil {
		t.Fatal("relative guest mount")
	}
	ms, err := prepareMounts([]vm.Mount{{Host: dir, Guest: "/work", Tag: "w"}})
	if err != nil || len(ms) != 1 || ms[0].Tag != "w" {
		t.Fatalf("%+v %v", ms, err)
	}
	ms, err = prepareMounts([]vm.Mount{{Host: dir, Guest: "/work2"}})
	if err != nil || ms[0].Tag == "" {
		t.Fatalf("%+v %v", ms, err)
	}
	if err := validateStoredMounts(ms); err != nil {
		t.Fatal(err)
	}
	if err := validateStoredMounts([]vm.Mount{{Host: "", Tag: ""}}); err == nil {
		t.Fatal("incomplete")
	}
	if err := validateStoredMounts([]vm.Mount{{Host: filepath.Join(dir, "nope"), Tag: "t"}}); err == nil {
		t.Fatal("missing host")
	}

	specs := mountSpecs(ms, "virtiofs")
	if len(specs) != 1 || specs[0].Driver != "virtiofs" {
		t.Fatalf("%+v", specs)
	}
	if mountSpecs(nil, "9p") != nil {
		t.Fatal()
	}
	specs = mountSpecs(ms, "")
	if specs[0].Driver != "9p" {
		t.Fatal(specs[0].Driver)
	}
	_ = cloudinit.MountSpec{}
}

func TestCopyAndPrepareForwardsCoverage(t *testing.T) {
	t.Parallel()
	if _, err := copyAndPrepareForwards(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := copyAndPrepareForwards([]vm.PortForward{{GuestPort: 0}}); err == nil {
		t.Fatal("guest 0")
	}
	// privileged host port rejected
	if _, err := copyAndPrepareForwards([]vm.PortForward{{GuestPort: 80, HostPort: 80}}); err == nil {
		t.Fatal("privileged")
	}
	got, err := copyAndPrepareForwards([]vm.PortForward{{GuestPort: 443, HostPort: 0}})
	if err != nil || got[0].HostPort == 0 {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = copyAndPrepareForwards([]vm.PortForward{{GuestPort: 80, HostPort: 18080}})
	if err != nil || got[0].HostPort != 18080 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestActiveStatusAndDiskExistsCoverage(t *testing.T) {
	t.Parallel()
	if !activeStatus(vm.StatusRunning) || !activeStatus(vm.StatusPaused) || !activeStatus(vm.StatusCreating) {
		t.Fatal("active")
	}
	if activeStatus(vm.StatusStopped) || activeStatus(vm.StatusSuspended) {
		t.Fatal("stopped")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if DiskExists(p) {
		t.Fatal("missing")
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !DiskExists(p) {
		t.Fatal("exists")
	}
}

func TestKillPIDNoopCoverage(t *testing.T) {
	t.Parallel()
	_ = killPID(0)
	_ = killPID(999999991)
}

func TestEmitCreateNilSafeCoverage(t *testing.T) {
	t.Parallel()
	emitCreate(vm.CreateOpts{}, vm.CreateEvent{Phase: "x"})
	var saw string
	emitCreate(vm.CreateOpts{OnEvent: func(ev vm.CreateEvent) { saw = ev.Phase }}, vm.CreateEvent{Phase: "y"})
	if saw != "y" {
		t.Fatal(saw)
	}
}

func TestAgentTargetCoverage(t *testing.T) {
	t.Parallel()
	if agentTarget(nil).HasEndpoint() {
		t.Fatal()
	}
	if !agentTarget(&vm.Instance{AgentPort: 1}).HasEndpoint() {
		t.Fatal()
	}
	if !agentTarget(&vm.Instance{AgentCID: 3}).HasEndpoint() {
		t.Fatal()
	}
}

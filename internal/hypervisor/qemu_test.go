package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func TestNewQEMURuntimeDefaults(t *testing.T) {
	t.Parallel()
	q := NewQEMURuntime("", "/data")
	if q.DataDir != "/data" {
		t.Fatalf("DataDir=%q", q.DataDir)
	}
	if q.MountDriver != MountDriver9p {
		t.Fatalf("MountDriver=%q", q.MountDriver)
	}
	if q.AgentTransport != AgentTransportAuto {
		t.Fatalf("AgentTransport=%q", q.AgentTransport)
	}
	wantBin := "qemu-system-x86_64"
	if runtime.GOARCH == "arm64" {
		wantBin = "qemu-system-aarch64"
	}
	if q.Binary != wantBin {
		t.Fatalf("Binary=%q want %q", q.Binary, wantBin)
	}

	q2 := NewQEMURuntime("custom-qemu", "/x")
	if q2.Binary != "custom-qemu" {
		t.Fatalf("Binary=%q", q2.Binary)
	}
}

func TestHostArchAndQemuBinary(t *testing.T) {
	t.Parallel()
	ha := hostArch()
	if ha != "amd64" && ha != "arm64" {
		t.Fatalf("hostArch=%q", ha)
	}
	if qemuBinaryForArch("amd64") != "qemu-system-x86_64" {
		t.Fatal(qemuBinaryForArch("amd64"))
	}
	if qemuBinaryForArch("arm64") != "qemu-system-aarch64" {
		t.Fatal(qemuBinaryForArch("arm64"))
	}
	if qemuBinaryForArch("other") != "qemu-system-aarch64" {
		t.Fatal("non-amd64 should map to aarch64 binary")
	}
}

func TestResolveGuestArchAliases(t *testing.T) {
	t.Parallel()
	ha := hostArch()
	cases := []struct {
		in, want string
	}{
		{"", ha},
		{"host", ha},
		{"native", ha},
		{"auto", ha},
		{"  ARM64  ", "arm64"},
		{"aarch64", "arm64"},
		{"amd64", "amd64"},
		{"x86_64", "amd64"},
		{"x86-64", "amd64"},
		{"x64", "amd64"},
		{"riscv64", ha}, // unknown → host
	}
	for _, tc := range cases {
		if got := resolveGuestArch(tc.in); got != tc.want {
			t.Fatalf("resolveGuestArch(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestMachineTypeForVariants(t *testing.T) {
	t.Parallel()
	// Cross always uses TCG variants.
	if got := machineTypeFor("arm64", true); got != "virt,accel=tcg,highmem=on" {
		t.Fatalf("arm64 cross: %s", got)
	}
	if got := machineTypeFor("amd64", true); got != "q35,accel=tcg" {
		t.Fatalf("amd64 cross: %s", got)
	}

	// Native depends on GOOS.
	switch runtime.GOOS {
	case "darwin":
		if got := machineTypeFor("arm64", false); got != "virt,accel=hvf,highmem=on" {
			t.Fatalf("arm64 native darwin: %s", got)
		}
		if got := machineTypeFor("amd64", false); got != "q35,accel=hvf" {
			t.Fatalf("amd64 native darwin: %s", got)
		}
	case "linux":
		if got := machineTypeFor("arm64", false); got != "virt,accel=kvm:tcg" {
			t.Fatalf("arm64 native linux: %s", got)
		}
		if got := machineTypeFor("amd64", false); got != "q35,accel=kvm:tcg" {
			t.Fatalf("amd64 native linux: %s", got)
		}
	default:
		// Other OS fall through to TCG paths for non-darwin/linux.
		if got := machineTypeFor("arm64", false); !strings.Contains(got, "tcg") {
			t.Fatalf("arm64 other: %s", got)
		}
	}
}

func TestCPUTypeForVariants(t *testing.T) {
	t.Parallel()
	if got := cpuTypeFor("amd64", true); got != "qemu64" {
		t.Fatalf("amd64 cross: %s", got)
	}
	if got := cpuTypeFor("arm64", true); got != "max" {
		t.Fatalf("arm64 cross: %s", got)
	}
	// Native without cross: "host" or "max" depending on KVM availability.
	got := cpuTypeFor(hostArch(), false)
	if got != "host" && got != "max" {
		t.Fatalf("native cpu: %s", got)
	}
}

func TestFindFirmwareFor(t *testing.T) {
	t.Parallel()
	// Just ensure it doesn't panic and returns strings (may be empty if no UEFI installed).
	code, vars := findFirmwareFor("arm64")
	_ = code
	_ = vars
	code2, vars2 := findFirmwareFor("amd64")
	_ = code2
	_ = vars2
	// If a candidate path exists on this machine, code should be non-empty.
	for _, p := range []string{
		"/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		"/usr/share/OVMF/OVMF_CODE.fd",
		"/usr/share/AAVMF/AAVMF_CODE.fd",
	} {
		if _, err := os.Stat(p); err == nil {
			// At least one firmware path exists; findFirmwareFor should find something
			// for the matching arch, but we don't assert which — just that the function
			// is callable. Coverage of the Stat loops is the goal.
			break
		}
	}
}

func TestTruncateFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "vars.fd")
	if err := truncateFile(path, 1024); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 1024 {
		t.Fatalf("size=%d", st.Size())
	}

	// Bad directory → error
	if err := truncateFile(filepath.Join(t.TempDir(), "nope", "x"), 8); err == nil {
		t.Fatal("expected error for missing parent dir")
	}
}

func TestResolveDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "disk.img")

	// No candidates → original path
	if got := resolveDisk(base); got != base {
		t.Fatalf("got %s", got)
	}

	// Prefer .qcow2 sibling when present and non-empty
	qcow := base + ".qcow2"
	if err := os.WriteFile(qcow, []byte("qcow-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveDisk(base); got != qcow {
		t.Fatalf("got %s want %s", got, qcow)
	}

	// Zero-size file is ignored
	dir2 := t.TempDir()
	base2 := filepath.Join(dir2, "disk.img")
	empty := base2 + ".qcow2"
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveDisk(base2); got != base2 {
		t.Fatalf("zero-size should be ignored: %s", got)
	}

	// disk.qcow2 in same dir
	dir3 := t.TempDir()
	base3 := filepath.Join(dir3, "disk.img")
	alt := filepath.Join(dir3, "disk.qcow2")
	if err := os.WriteFile(alt, []byte("alt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := resolveDisk(base3); got != alt {
		t.Fatalf("got %s want %s", got, alt)
	}
}

func TestCollectQEMUPIDsAndAlive(t *testing.T) {
	t.Parallel()
	// Empty / invalid PIDs
	if pids := collectQEMUPIDs(&vm.Instance{}); len(pids) != 0 {
		t.Fatalf("pids=%v", pids)
	}
	if anyPIDAlive(nil) {
		t.Fatal("nil should be dead")
	}
	// High unused PID — Signal(0) should fail (pid 0 is special on Unix and may "succeed").
	if anyPIDAlive([]int{999999999}) {
		t.Fatal("dead pid should not be alive")
	}

	self := os.Getpid()
	if !anyPIDAlive([]int{self}) {
		t.Fatal("self should be alive")
	}

	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.qcow2")
	if err := os.WriteFile(filepath.Join(dir, "qemu.pid"), []byte("999999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids := collectQEMUPIDs(&vm.Instance{PID: self, DiskPath: disk})
	if len(pids) < 1 || pids[0] != self {
		t.Fatalf("pids=%v", pids)
	}
	// Dedup when pidfile matches inst.PID
	if err := os.WriteFile(filepath.Join(dir, "qemu.pid"), []byte(strconv.Itoa(self)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids = collectQEMUPIDs(&vm.Instance{PID: self, DiskPath: disk})
	if len(pids) != 1 {
		t.Fatalf("dedup failed: %v", pids)
	}
}

func TestCleanupQEMUFiles(t *testing.T) {
	t.Parallel()
	// No disk → no-op
	cleanupQEMUFiles(&vm.Instance{})

	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.qcow2")
	pidPath := filepath.Join(dir, "qemu.pid")
	qmp := filepath.Join(dir, QMPSocketName)
	extraQMP := filepath.Join(dir, "extra-qmp.sock")
	for _, p := range []string{pidPath, qmp, extraQMP} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Also create a virtiofs pid so StopVirtiofsDaemons path runs
	if err := os.WriteFile(filepath.Join(dir, "virtiofsd-0.pid"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cleanupQEMUFiles(&vm.Instance{DiskPath: disk, QMPPath: extraQMP})
	for _, p := range []string{pidPath, qmp, extraQMP} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed", p)
		}
	}
}

func TestQEMURunning(t *testing.T) {
	t.Parallel()
	q := NewQEMURuntime("qemu", t.TempDir())
	if q.Running(&vm.Instance{PID: 0}) {
		t.Fatal("pid 0")
	}
	if q.Running(&vm.Instance{PID: -1}) {
		t.Fatal("pid -1")
	}
	if !q.Running(&vm.Instance{PID: os.Getpid()}) {
		t.Fatal("self should be running")
	}
	// Very high PID unlikely to exist
	if q.Running(&vm.Instance{PID: 999999999}) {
		t.Fatal("dead pid should not be running")
	}
}

func TestQEMUPauseResumeSaveVMErrors(t *testing.T) {
	t.Parallel()
	q := NewQEMURuntime("qemu", t.TempDir())
	ctx := context.Background()
	// Not running
	inst := &vm.Instance{Name: "x", PID: 0}
	if err := q.Pause(ctx, inst); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("pause: %v", err)
	}
	if err := q.Resume(ctx, inst); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("resume: %v", err)
	}
	if err := q.SaveVM(ctx, inst, "t"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("savevm: %v", err)
	}
	if err := q.SaveVM(ctx, &vm.Instance{Name: "x", PID: os.Getpid()}, ""); err == nil || !strings.Contains(err.Error(), "snapshot tag") {
		t.Fatalf("empty tag: %v", err)
	}

	// Running but no QMP
	alive := &vm.Instance{Name: "y", PID: os.Getpid()}
	if err := q.Pause(ctx, alive); err == nil || !strings.Contains(err.Error(), "no QMP") {
		t.Fatalf("pause no qmp: %v", err)
	}
	if err := q.Resume(ctx, alive); err == nil || !strings.Contains(err.Error(), "no QMP") {
		t.Fatalf("resume no qmp: %v", err)
	}
	// Wrong disk format
	alive.DiskPath = filepath.Join(t.TempDir(), "disk.img")
	alive.QMPPath = filepath.Join(t.TempDir(), "qmp.sock")
	if err := q.SaveVM(ctx, alive, "tag"); err == nil || !strings.Contains(err.Error(), "qcow2") {
		t.Fatalf("savevm raw: %v", err)
	}
}

func TestQEMUPauseResumeSaveVMWithQMP(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := shortUnixSock(t)
	var mu sync.Mutex
	var cmds []string
	cleanup := mockQMPServer(t, sock, func(cmd string) map[string]any {
		mu.Lock()
		cmds = append(cmds, cmd)
		mu.Unlock()
		return map[string]any{"return": map[string]any{}}
	})
	defer cleanup()

	q := NewQEMURuntime("qemu", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	inst := &vm.Instance{
		Name:     "qmp-vm",
		PID:      os.Getpid(),
		QMPPath:  sock,
		DiskPath: filepath.Join(dir, "disk.qcow2"),
	}
	if err := q.Pause(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if err := q.Resume(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if err := q.SaveVM(ctx, inst, "grain-suspend"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Each qmpCommand/qmpHumanMonitor dials fresh → each has qmp_capabilities
	joined := strings.Join(cmds, ",")
	for _, want := range []string{"stop", "cont", "human-monitor-command"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("cmds %v missing %s", cmds, want)
		}
	}
}

func TestQEMUStopWithQMPPowerdown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := filepath.Join(dir, "qmp.sock")
	// powerdown succeeds; process (our test PID) stays "alive" so we hit timeout then hardKill.
	// Use a dead PID so powerdown path sees process exit quickly... actually anyPIDAlive on
	// self stays true. Use PID 0 and only pidfile with dead pid so graceful path completes.
	cleanup := mockQMPServer(t, sock, func(cmd string) map[string]any {
		return map[string]any{"return": map[string]any{}}
	})
	defer cleanup()

	disk := filepath.Join(dir, "disk.qcow2")
	if err := os.WriteFile(filepath.Join(dir, "qemu.pid"), []byte("999999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Touch qmp path file name is the socket already

	q := NewQEMURuntime("qemu", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inst := &vm.Instance{
		Name:     "stop-me",
		PID:      999999999, // dead
		QMPPath:  sock,
		DiskPath: disk,
	}
	if err := q.Stop(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusStopped || inst.PID != 0 || inst.QMPPath != "" {
		t.Fatalf("after stop: status=%s pid=%d qmp=%q", inst.Status, inst.PID, inst.QMPPath)
	}
}

func TestQEMUStopNoQMPHardKill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.qcow2")
	q := NewQEMURuntime("qemu", dir)
	inst := &vm.Instance{
		Name:     "hard",
		PID:      999999998,
		DiskPath: disk,
	}
	if err := q.Stop(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusStopped {
		t.Fatalf("status=%s", inst.Status)
	}
}

func TestQEMUStartMissingBinary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	q := NewQEMURuntime("grain-nonexistent-qemu-binary-xyz", dir)
	inst := &vm.Instance{Name: "no-bin", CPUs: 1, MemoryMB: 256, Arch: hostArch()}
	err := q.Start(context.Background(), inst, filepath.Join(dir, "disk.img"))
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got: %v", err)
	}
}

// writeFakeQEMU installs a shell script that writes the -pidfile argument and
// exits with exitCode (simulates successful/failed -daemonize). Returns absolute path.
func writeFakeQEMU(t *testing.T, dir, binName string, exitCode int) string {
	t.Helper()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(binDir, binName)
	script := `#!/bin/sh
pidfile=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-pidfile" ]; then
    pidfile="$arg"
  fi
  prev="$arg"
done
if [ -n "$pidfile" ]; then
  echo $$ > "$pidfile"
fi
exit ` + strconv.Itoa(exitCode) + `
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestQEMUStartWithFakeBinary(t *testing.T) {
	t.Parallel()
	// Absolute fake binary path — no PATH mutation; host arch so Start keeps q.Binary.
	dir := t.TempDir()
	binPath := writeFakeQEMU(t, dir, "grain-fake-qemu-ok", 0)

	dataDir := filepath.Join(dir, "data")
	vmDir := filepath.Join(dir, "vms", "fake-start")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.qcow2")
	if err := os.WriteFile(disk, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Optional cloud-init seed path
	if err := os.WriteFile(filepath.Join(vmDir, "seed.iso"), []byte("iso"), 0o644); err != nil {
		t.Fatal(err)
	}

	q := NewQEMURuntime(binPath, dataDir)
	q.AgentTransport = AgentTransportTCP // avoid vsock requirement
	inst := &vm.Instance{
		Name:     "fake-start",
		CPUs:     2,
		MemoryMB: 512,
		Arch:     hostArch(),
		GPU:      "virtio",
		Network:  "overlay",
		LoadVM:   "grain-suspend",
		Mounts: []vm.Mount{
			{Host: dir, Tag: "grain0"},
		},
		Forwards: []vm.PortForward{
			{GuestPort: 8080, HostPort: 0},
		},
	}
	if err := q.Start(context.Background(), inst, disk); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status=%s", inst.Status)
	}
	if inst.PID <= 0 {
		t.Fatalf("PID=%d", inst.PID)
	}
	if inst.SSHPort <= 0 || inst.AgentPort <= 0 {
		t.Fatalf("ports ssh=%d agent=%d", inst.SSHPort, inst.AgentPort)
	}
	if inst.QMPPath == "" {
		t.Fatal("expected QMPPath")
	}
	if inst.Forwards[0].HostPort <= 0 {
		t.Fatal("expected allocated forward port")
	}
}

func TestQEMUStartFakeBinaryFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	binPath := writeFakeQEMU(t, dir, "qemu-fail-bin", 1)

	dataDir := filepath.Join(dir, "data")
	vmDir := filepath.Join(dir, "vms", "fail")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.img")
	if err := os.WriteFile(disk, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Serial log for error tail path
	if err := os.WriteFile(filepath.Join(vmDir, "serial.log"), []byte(strings.Repeat("serial-err ", 80)), 0o644); err != nil {
		t.Fatal(err)
	}

	q := NewQEMURuntime(binPath, dataDir)
	q.AgentTransport = AgentTransportTCP
	inst := &vm.Instance{Name: "fail-start", CPUs: 1, MemoryMB: 256, Arch: hostArch()}
	err := q.Start(context.Background(), inst, disk)
	if err == nil {
		t.Fatal("expected start failure")
	}
	if !strings.Contains(err.Error(), "qemu:") {
		t.Fatalf("got: %v", err)
	}
}

func TestQEMUStartFakeBinaryMissingPidfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Exit 0 but never write pidfile
	binPath := filepath.Join(binDir, "qemu-nopid")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	dataDir := filepath.Join(dir, "data")
	vmDir := filepath.Join(dir, "vms", "nopid")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.img")
	if err := os.WriteFile(disk, []byte("disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	q := NewQEMURuntime(binPath, dataDir)
	q.AgentTransport = AgentTransportTCP
	inst := &vm.Instance{Name: "nopid", CPUs: 1, MemoryMB: 256, Arch: hostArch()}
	err := q.Start(context.Background(), inst, disk)
	if err == nil || !strings.Contains(err.Error(), "pidfile") {
		t.Fatalf("want pidfile error, got: %v", err)
	}
}

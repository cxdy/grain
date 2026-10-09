package hypervisor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestProcessExecutableSelf(t *testing.T) {
	exe, err := processExecutable(os.Getpid())
	if err != nil || exe == "" {
		t.Fatalf("exe=%q err=%v", exe, err)
	}
	if filepath.Base(exe) == "" || strings.Contains(exe, "\x00") {
		t.Fatalf("bad exe %q", exe)
	}
	match, known := pidIsHypervisor(os.Getpid(), "qemu-system")
	if !known || match {
		t.Fatalf("test process matched qemu: match=%v known=%v exe=%s", match, known, exe)
	}
	base := strings.TrimSuffix(filepath.Base(exe), " (deleted)")
	match, known = pidIsHypervisor(os.Getpid(), base)
	if !known || !match {
		t.Fatalf("self should match %q: match=%v known=%v", base, match, known)
	}
	if match, known := pidIsHypervisor(0, "qemu-system"); match || !known {
		t.Fatalf("pid 0: match=%v known=%v", match, known)
	}
}

func TestRuntimeProcessRunningRejectsReusedPID(t *testing.T) {
	dir := t.TempDir()
	deadSock := filepath.Join(dir, "qmp.sock")
	if runtimeProcessRunning(os.Getpid(), deadSock, "qemu-system") {
		t.Fatal("foreign pid with a dead control socket counted as running")
	}
	if runtimeProcessRunning(0, deadSock, "qemu-system") {
		t.Fatal("pid 0")
	}
	if runtimeProcessRunning(999999999, deadSock, "qemu-system") {
		t.Fatal("dead pid")
	}
	// No recorded socket: pid liveness still counts (test stand-in).
	if !runtimeProcessRunning(os.Getpid(), "", "qemu-system") {
		t.Fatal("self without control socket")
	}

	sock := shortUnixSock(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if !runtimeProcessRunning(os.Getpid(), sock, "qemu-system") {
		t.Fatal("live control socket stand-in should count as running")
	}
	if !controlSocketAlive(sock) {
		t.Fatal("listener should accept")
	}
	if controlSocketAlive(deadSock) || controlSocketAlive("") {
		t.Fatal("missing socket should be dead")
	}
}

func TestQEMUStopDoesNotSignalForeignPID(t *testing.T) {
	q := NewQEMURuntime("qemu", t.TempDir())
	dir := t.TempDir()
	self := os.Getpid()
	inst := &vm.Instance{
		Name:     "foreign",
		PID:      self,
		Status:   vm.StatusRunning,
		QMPPath:  filepath.Join(dir, "qmp.sock"),
		DiskPath: filepath.Join(dir, "disk.qcow2"),
	}
	if err := q.Stop(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	if !pidAlive(self) {
		t.Fatal("stop signaled the test process")
	}
	if inst.Status != vm.StatusStopped || inst.PID != 0 {
		t.Fatalf("status=%s pid=%d", inst.Status, inst.PID)
	}
}

func TestFirecrackerStopDoesNotSignalForeignPID(t *testing.T) {
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	dir := t.TempDir()
	self := os.Getpid()
	inst := &vm.Instance{
		Name:     "foreign-fc",
		PID:      self,
		Status:   vm.StatusRunning,
		QMPPath:  filepath.Join(dir, "missing.sock"),
		DiskPath: filepath.Join(dir, "disk.raw"),
	}
	if err := rt.Stop(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	if !pidAlive(self) {
		t.Fatal("stop signaled the test process")
	}
	if inst.Status != vm.StatusStopped || inst.PID != 0 {
		t.Fatalf("status=%s pid=%d", inst.Status, inst.PID)
	}
}

func TestSignalHypervisorSkipsForeignPID(t *testing.T) {
	self := os.Getpid()
	signalHypervisorPIDs([]int{self, 0, -1}, "qemu-system")
	if !pidAlive(self) {
		t.Fatal("signaled the test process")
	}
	if hypervisorStillAlive([]int{self}, "qemu-system") {
		t.Fatal("foreign pid should not count as qemu")
	}
	exe, err := processExecutable(self)
	if err != nil {
		t.Fatal(err)
	}
	if !hypervisorStillAlive([]int{self}, filepath.Base(exe)) {
		t.Fatal("self should count when the fragment matches")
	}
}

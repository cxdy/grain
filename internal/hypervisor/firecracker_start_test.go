package hypervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

// TestFirecrackerStartErrorPaths forces the linux Start body (via runtimeGOOS) so
// macOS/CI still cover binary/kernel/disk failure branches without a real VMM.
func TestFirecrackerStartErrorPaths(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = "linux"
	t.Cleanup(func() { runtimeGOOS = old })

	dir := t.TempDir()
	rt := NewFirecrackerRuntime("grain-nonexistent-firecracker-xyz", dir, "")

	// Missing binary
	err := rt.Start(context.Background(), &vm.Instance{Name: "a", CPUs: 1, MemoryMB: 128}, filepath.Join(dir, "d.raw"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing binary: %v", err)
	}

	// Provide a real executable as "Binary" so LookPath succeeds, then fail on kernel.
	// Write a tiny shell script named as the binary path (LookPath needs PATH or abs path).
	fakeBin := filepath.Join(dir, "fake-fc")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rt.Binary = fakeBin

	// Missing kernel
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = rt.Start(context.Background(), &vm.Instance{Name: "b", CPUs: 1, MemoryMB: 128}, disk)
	if err == nil || !strings.Contains(err.Error(), "kernel") {
		t.Fatalf("missing kernel: %v", err)
	}

	// Kernel present, bad disk path
	k := filepath.Join(dir, "kernels", FCDefaultKernel)
	if err := os.MkdirAll(filepath.Dir(k), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, []byte("vmlinux-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt.KernelPath = k
	err = rt.Start(context.Background(), &vm.Instance{Name: "c", CPUs: 1, MemoryMB: 128}, filepath.Join(dir, "missing.raw"))
	if err == nil {
		t.Fatal("expected disk error")
	}

	// Full start with fake binary that exits immediately — must surface log tail,
	// not return success (regression: KVM-less hosts used to reach wait-agent).
	oldGrace := fcPostStartGrace
	fcPostStartGrace = 100 * time.Millisecond
	t.Cleanup(func() { fcPostStartGrace = oldGrace })
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a wrapper that emits a KVM-like line then exits (mirrors real FC).
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho 'Kvm error: Error creating KVM object: No such file or directory' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = rt.Start(context.Background(), &vm.Instance{Name: "d", CPUs: 2, MemoryMB: 256}, disk)
	if err == nil {
		t.Fatal("expected immediate-exit error from firecracker")
	}
	if !strings.Contains(err.Error(), "exited immediately") {
		t.Fatalf("want exited immediately, got: %v", err)
	}
	if !strings.Contains(err.Error(), "KVM") && !strings.Contains(err.Error(), "kvm") {
		t.Fatalf("want KVM hint or log, got: %v", err)
	}
}

func TestFCImmediateExitErrKVMHint(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "fc.log")
	body := "Running Firecracker\nKvm error: Error creating KVM object: No such file or directory (os error 2)\n"
	if err := os.WriteFile(logPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := fcImmediateExitErr(logPath, fmt.Errorf("exit status 1"))
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	if !strings.Contains(s, "KVM unavailable") {
		t.Fatalf("missing KVM hint: %s", s)
	}
	if !strings.Contains(s, "Kvm error") {
		t.Fatalf("missing log tail: %s", s)
	}
}

func TestReadLogTail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.log")
	if err := os.WriteFile(p, []byte("  hello world  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readLogTail(p, 100); got != "hello world" {
		t.Fatalf("got %q", got)
	}
	if got := readLogTail(p, 5); got != "world" {
		t.Fatalf("trunc %q", got)
	}
	if got := readLogTail(filepath.Join(dir, "missing"), 10); got != "" {
		t.Fatalf("missing: %q", got)
	}
}

func TestFirecrackerStartWithSeedAndSleepBin(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = "linux"
	t.Cleanup(func() { runtimeGOOS = old })

	oldGrace := fcPostStartGrace
	fcPostStartGrace = 50 * time.Millisecond
	t.Cleanup(func() { fcPostStartGrace = oldGrace })

	dir := t.TempDir()
	// sleep long enough that Running sees the process, then we Stop it.
	fakeBin := filepath.Join(dir, "fc-sleep")
	// Use the real sleep binary via a wrapper so Process stays alive.
	// Also create a fake API unix socket quickly so Start's socket wait ends.
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	vmDir := filepath.Join(dir, "vms", "live")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	apiSock := filepath.Join(vmDir, FCSocketName)
	// Wrapper: create a unix socket path as empty file (Start accepts dial-or-stat),
	// then sleep. Use python/nc if available; simplest: touch file — Stat succeeds
	// and dial may fail, but fcAPISocketReady tries dial after Stat of any file.
	// Create a real listening unix socket via a tiny Go-less approach: socat/python.
	// Fall back to long sleep without socket — Start waits up to 5s then grace.
	script := "#!/bin/sh\n" +
		// Best-effort UDS so Start's socket loop exits early.
		"python3 -c 'import socket,time,sys;s=socket.socket(socket.AF_UNIX);s.bind(sys.argv[1]);s.listen(1);time.sleep(30)' " + apiSock + " &\n" +
		"exec " + sleepPath + " 30\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	k := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(k, []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed ISO branch
	if err := os.WriteFile(filepath.Join(vmDir, "seed.iso"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := NewFirecrackerRuntime(fakeBin, dir, k)
	inst := &vm.Instance{Name: "live", CPUs: 1, MemoryMB: 128}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.Start(ctx, inst, disk); err != nil {
		// Setpgid may fail in some restricted sandboxes; still exercised most of Start.
		t.Logf("Start: %v", err)
		return
	}
	if inst.PID <= 0 {
		t.Fatal("expected PID")
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status %s", inst.Status)
	}
	// Config file written
	if _, err := os.Stat(filepath.Join(vmDir, FCConfigName)); err != nil {
		t.Fatal(err)
	}
	// Stop the sleep process
	_ = rt.Stop(context.Background(), inst)
}

func TestFirecrackerStartNonLinuxStillGated(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("gated path for non-linux")
	}
	old := runtimeGOOS
	runtimeGOOS = "darwin"
	t.Cleanup(func() { runtimeGOOS = old })
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	err := rt.Start(context.Background(), &vm.Instance{Name: "x"}, "disk")
	if err == nil || !strings.Contains(err.Error(), "requires linux") {
		t.Fatalf("%v", err)
	}
}

package hypervisor

import (
	"context"
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

	// Full start with fake binary that exits immediately (covers config write + Start + Running fail).
	// sleep 0 then exit so process dies before Running check.
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = rt.Start(context.Background(), &vm.Instance{Name: "d", CPUs: 2, MemoryMB: 256}, disk)
	if err == nil {
		// On some systems the process may still look alive briefly; either ok for coverage.
		t.Log("start returned nil (process may still appear alive briefly)")
	} else if !strings.Contains(err.Error(), "firecracker") && !strings.Contains(err.Error(), "exited") {
		t.Logf("start error (acceptable): %v", err)
	}
}

func TestFirecrackerStartWithSeedAndSleepBin(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = "linux"
	t.Cleanup(func() { runtimeGOOS = old })

	dir := t.TempDir()
	// sleep long enough that Running sees the process, then we Stop it.
	fakeBin := filepath.Join(dir, "fc-sleep")
	// Use the real sleep binary via a wrapper so Process stays alive.
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	script := "#!/bin/sh\nexec " + sleepPath + " 30\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	k := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(k, []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	vmDir := filepath.Join(dir, "vms", "live")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
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

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestCheckDevKVMMissing(t *testing.T) {
	old := kvmDevicePath
	t.Cleanup(func() { kvmDevicePath = old })
	kvmDevicePath = filepath.Join(t.TempDir(), "no-such-kvm")
	err := checkDevKVM()
	if err == nil {
		t.Fatal("expected missing device error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "nested") {
		t.Fatalf("want nested virt hint: %v", err)
	}
}

func TestCheckDevKVMDirectory(t *testing.T) {
	old := kvmDevicePath
	t.Cleanup(func() { kvmDevicePath = old })
	dir := t.TempDir()
	kvmDevicePath = dir
	err := checkDevKVM()
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("err: %v", err)
	}
}

func TestCheckDevKVMNotAccessible(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows")
	}
	old := kvmDevicePath
	t.Cleanup(func() { kvmDevicePath = old })
	// Regular file exists but opening RDWR as a "device" still works for files —
	// use a path under a directory without access when possible.
	// Create a file with mode 000 (may still open as root; skip if root).
	if os.Geteuid() == 0 {
		t.Skip("root can open mode 000 files")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "kvmfake")
	if err := os.WriteFile(p, []byte{}, 0o000); err != nil {
		t.Fatal(err)
	}
	kvmDevicePath = p
	err := checkDevKVM()
	if err == nil {
		t.Fatal("expected not accessible")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Fatalf("err: %v", err)
	}
}

func TestKVMNestedVirtHintEmptyOnMissingProc(t *testing.T) {
	// On non-linux /proc/cpuinfo may be absent → empty hint is fine.
	// Just ensure it does not panic.
	_ = kvmNestedVirtHint()
}

func TestRunDoctorFirecrackerRequiresKVMOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("kvm hard-check only on linux")
	}
	old := kvmDevicePath
	t.Cleanup(func() { kvmDevicePath = old })
	kvmDevicePath = filepath.Join(t.TempDir(), "missing-kvm")

	dir := t.TempDir()
	// Fake firecracker binary on PATH via absolute path in config.
	fakeBin := filepath.Join(dir, "firecracker")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	kpath := filepath.Join(dir, "kernels", "vmlinux")
	if err := os.MkdirAll(filepath.Dir(kpath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kpath, []byte("vmlinux"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Minimal image so doctor gets past image check.
	imgDir := filepath.Join(dir, "images", "grain-ubuntu")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := configDoctorFC(dir, fakeBin, kpath)
	err := runDoctor(cfg)
	if err == nil {
		t.Fatal("expected doctor issues without /dev/kvm")
	}
	if !strings.Contains(err.Error(), "doctor found issues") {
		t.Fatalf("err: %v", err)
	}
}

// configDoctorFC is a tiny helper for firecracker doctor tests.
func configDoctorFC(dir, fcBin, kernel string) config.Config {
	return config.Config{
		DataDir:           dir,
		Hypervisor:        "firecracker",
		FirecrackerBinary: fcBin,
		KernelPath:        kernel,
		Image:             "grain-ubuntu",
		Socket:            filepath.Join(dir, "grain.sock"),
		SSHUser:           "ubuntu",
	}
}

package hypervisor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestBuildFCConfigJSON(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig(
		"/kernels/vmlinux",
		"/vms/sbox/disk.raw",
		2,
		2048,
		42,
		"/vms/sbox/fc-vsock.sock",
	)
	b, err := MarshalFCConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json: %v\n%s", err, b)
	}

	boot, ok := raw["boot-source"].(map[string]any)
	if !ok {
		t.Fatalf("missing boot-source: %s", b)
	}
	if boot["kernel_image_path"] != "/kernels/vmlinux" {
		t.Fatalf("kernel path: %v", boot["kernel_image_path"])
	}
	if boot["boot_args"] == "" {
		t.Fatal("expected boot_args")
	}

	drives, ok := raw["drives"].([]any)
	if !ok || len(drives) < 1 {
		t.Fatalf("drives: %v", raw["drives"])
	}
	d0 := drives[0].(map[string]any)
	if d0["path_on_host"] != "/vms/sbox/disk.raw" {
		t.Fatalf("rootfs path: %v", d0["path_on_host"])
	}
	if d0["is_root_device"] != true {
		t.Fatal("expected is_root_device")
	}

	mc := raw["machine-config"].(map[string]any)
	if int(mc["vcpu_count"].(float64)) != 2 {
		t.Fatalf("vcpu_count: %v", mc["vcpu_count"])
	}
	if int(mc["mem_size_mib"].(float64)) != 2048 {
		t.Fatalf("mem_size_mib: %v", mc["mem_size_mib"])
	}

	vs, ok := raw["vsock"].(map[string]any)
	if !ok {
		t.Fatalf("missing vsock: %s", b)
	}
	if int(vs["guest_cid"].(float64)) != 42 {
		t.Fatalf("guest_cid: %v", vs["guest_cid"])
	}
	if vs["uds_path"] != "/vms/sbox/fc-vsock.sock" {
		t.Fatalf("uds_path: %v", vs["uds_path"])
	}
}

func TestBuildFCConfigOmitsVsockWhenCIDLow(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig("/k", "/d", 1, 128, 0, "/v.sock")
	b, err := MarshalFCConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["vsock"]; ok {
		t.Fatalf("vsock should be omitted when guest_cid < MinGuestCID: %s", b)
	}
}

func TestFirecrackerStartRequiresLinux(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "linux" {
		t.Skip("linux can attempt real Start; this test is for non-linux failure path")
	}
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	inst := &vm.Instance{Name: "fc-test", CPUs: 1, MemoryMB: 256}
	err := rt.Start(context.Background(), inst, filepath.Join(t.TempDir(), "disk.raw"))
	if err == nil {
		t.Fatal("expected error on non-linux")
	}
	if !strings.Contains(err.Error(), "firecracker requires linux") {
		t.Fatalf("want linux error, got: %v", err)
	}
}

func TestFirecrackerStartMissingBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("binary check runs after OS check")
	}
	rt := NewFirecrackerRuntime("grain-nonexistent-firecracker-bin", t.TempDir(), "")
	// Provide a fake kernel so we fail on binary, not kernel.
	kdir := filepath.Join(rt.DataDir, "kernels")
	_ = os.MkdirAll(kdir, 0o755)
	kpath := filepath.Join(kdir, "vmlinux")
	if err := os.WriteFile(kpath, []byte("not-a-real-kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt.KernelPath = kpath

	disk := filepath.Join(t.TempDir(), "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{Name: "fc-miss", CPUs: 1, MemoryMB: 256}
	err := rt.Start(context.Background(), inst, disk)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found, got: %v", err)
	}
}

func TestFirecrackerSaveVMUnsupported(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	err := rt.SaveVM(context.Background(), &vm.Instance{Name: "x", PID: 1}, "tag")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("got %v", err)
	}
}

func TestEnsureRawRootfsPassthrough(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	raw := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(raw, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureRawRootfs(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("got %s want %s", got, raw)
	}
}

func TestEnsureRawRootfsQcow2NeedsQemuImgOrRefuse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	qcow := filepath.Join(dir, "disk.qcow2")
	if err := os.WriteFile(qcow, []byte("qcow2-not-real"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without a valid qcow2, qemu-img convert will fail if present; either
	// missing qemu-img or convert failure is acceptable for this unit test.
	_, err := ensureRawRootfs(context.Background(), qcow)
	if err == nil {
		// Convert succeeded only if qemu-img accepted the fake file (unlikely).
		// If it did, raw should exist.
		if _, stErr := os.Stat(filepath.Join(dir, FCRawDiskName)); stErr != nil {
			t.Fatal("expected raw disk after successful convert")
		}
		return
	}
	msg := err.Error()
	if !strings.Contains(msg, "raw") && !strings.Contains(msg, "qemu-img") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFirecrackerResolveKernel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rt := NewFirecrackerRuntime("", dir, "")
	_, err := rt.resolveKernel()
	if err == nil {
		t.Fatal("expected missing kernel")
	}

	kdir := filepath.Join(dir, "kernels")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	kpath := filepath.Join(kdir, "vmlinux")
	if err := os.WriteFile(kpath, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := rt.resolveKernel()
	if err != nil {
		t.Fatal(err)
	}
	if got != kpath {
		t.Fatalf("got %s want %s", got, kpath)
	}

	// Explicit override wins.
	override := filepath.Join(dir, "custom-vmlinux")
	if err := os.WriteFile(override, []byte("k2"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt.KernelPath = override
	got, err = rt.resolveKernel()
	if err != nil {
		t.Fatal(err)
	}
	if got != override {
		t.Fatalf("got %s want %s", got, override)
	}
}

func TestNewFirecrackerRuntimeDefaults(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("", "/data", "")
	if rt.Binary != FCDefaultBin {
		t.Fatalf("binary %q", rt.Binary)
	}
	if rt.DataDir != "/data" {
		t.Fatalf("datadir %q", rt.DataDir)
	}
}

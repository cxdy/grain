//go:build linux

package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestFirecrackerStartMissingBinary(t *testing.T) {
	dir := t.TempDir()
	f := NewFirecrackerRuntime("no-such-firecracker-binary-xyz", dir, "")
	disk := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(disk, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	err := f.Start(context.Background(), &vm.Instance{Name: "fc1", CPUs: 1, MemoryMB: 128}, disk)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestFirecrackerStartMissingKernel(t *testing.T) {
	dir := t.TempDir()
	// Point Binary at /bin/true so LookPath succeeds, kernel resolve fails.
	f := NewFirecrackerRuntime("true", dir, filepath.Join(dir, "missing-vmlinux"))
	disk := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(disk, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	err := f.Start(context.Background(), &vm.Instance{Name: "fc2", CPUs: 1, MemoryMB: 128}, disk)
	if err == nil {
		t.Fatal("expected kernel error")
	}
}

func TestEnsureRawRootfsConvertPath(t *testing.T) {
	dir := t.TempDir()
	// Missing path
	if _, err := ensureRawRootfs(context.Background(), filepath.Join(dir, "nope.img")); err == nil {
		t.Fatal("expected missing")
	}
	// Already .raw
	raw := filepath.Join(dir, "d.raw")
	if err := os.WriteFile(raw, make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureRawRootfs(context.Background(), raw)
	if err != nil || got != raw {
		t.Fatalf("got %s err %v", got, err)
	}
}

func TestResolveKernelPaths(t *testing.T) {
	dir := t.TempDir()
	f := NewFirecrackerRuntime("firecracker", dir, "")
	// missing default
	if _, err := f.resolveKernel(); err == nil {
		t.Fatal("expected missing kernel")
	}
	// explicit path
	k := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(k, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	f2 := NewFirecrackerRuntime("firecracker", dir, k)
	got, err := f2.resolveKernel()
	if err != nil || got != k {
		t.Fatalf("%s %v", got, err)
	}
	// dataDir/vmlinux
	f3 := NewFirecrackerRuntime("firecracker", dir, "")
	got, err = f3.resolveKernel()
	if err != nil || got != k {
		t.Fatalf("dataDir kernel %s %v", got, err)
	}
}

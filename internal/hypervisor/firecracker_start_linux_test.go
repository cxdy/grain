//go:build linux

package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestFirecrackerStartMissingKernel(t *testing.T) {
	dir := t.TempDir()
	// Binary exists on PATH (true); kernel path missing after ensureRawRootfs.
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

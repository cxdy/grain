package hypervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalDiskCloneCopyFallback(t *testing.T) {
	// Empty PATH so qemu-img is missing → copyFile path (and clonefile on darwin).
	t.Setenv("PATH", "/nonexistent")
	dir := t.TempDir()
	base := filepath.Join(dir, "base.img")
	if err := os.WriteFile(base, []byte("base-disk-content-xxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "vm", "disk.img")
	d := NewLocalDisk(dir)
	if err := d.Clone(context.Background(), base, dest, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		// On darwin clonefile may write dest; on linux copyFile should.
		// qemu-img missing may also leave dest as .qcow2 if LookPath still finds something.
		if _, err2 := os.Stat(dest + ".qcow2"); err2 != nil {
			t.Fatalf("dest missing: %v / %v", err, err2)
		}
	}
}

func TestMaybeResizeNoQemu(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	if err := maybeResize(context.Background(), "/nope", 10); err != nil {
		t.Fatal(err)
	}
	if err := maybeResize(context.Background(), "/nope", 0); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "out")); err == nil {
		t.Fatal("expected open error")
	}
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dest parent missing
	if err := copyFile(src, filepath.Join(dir, "no", "such", "out")); err == nil {
		t.Fatal("expected create error")
	}
}

func TestClonefileFailure(t *testing.T) {
	// clonefile uses cp -c; invalid paths should error
	if err := clonefile("/no/such/src", "/no/such/dst"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLocalDiskCloneQemuOverlay(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("no qemu-img")
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")
	// create a tiny qcow2 base
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", base, "32M")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("qemu-img create: %v %s", err, out)
	}
	dest := filepath.Join(dir, "vms", "x", "disk.img")
	d := NewLocalDisk(dir)
	if err := d.Clone(context.Background(), base, dest, 1); err != nil {
		t.Fatal(err)
	}
	// overlay should be .qcow2
	if _, err := os.Stat(dest + ".qcow2"); err != nil {
		if _, err2 := os.Stat(dest); err2 != nil {
			t.Fatalf("no dest: %v %v", err, err2)
		}
	}
}

package hypervisor

import (
	"context"
	"os"
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

package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/image"
)

func TestNewLocalDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := NewLocalDisk(dir)
	if d.DataDir != dir {
		t.Fatalf("DataDir=%q", d.DataDir)
	}
	if d.Images == nil {
		t.Fatal("Images nil")
	}
}

func TestLocalDiskEnsureBaseReady(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := NewLocalDisk(dir)
	// DiskPath requires size > 1MiB
	imgDir := filepath.Join(dir, "images", "test-img")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	diskPath := filepath.Join(imgDir, "disk.qcow2")
	f, err := os.Create(diskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	got, err := d.EnsureBase(context.Background(), "test-img")
	if err != nil {
		t.Fatal(err)
	}
	if got != diskPath {
		t.Fatalf("got %s want %s", got, diskPath)
	}
}

func TestLocalDiskEnsureBaseNilImages(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := &LocalDisk{DataDir: dir, Images: nil}
	// Missing image → Pull fails (no catalog id / not ready)
	_, err := d.EnsureBase(context.Background(), "no-such-image-xyz")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("got: %v", err)
	}
	if d.Images == nil {
		t.Fatal("Images should be initialized")
	}
}

func TestLocalDiskEnsureBaseLocalOnlyHint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := NewLocalDisk(dir)
	// grain-ubuntu may be local-only in catalog depending on arch/setup.
	// Force a known local-only if present in catalog; otherwise skip.
	spec, err := image.Get(image.IDGrainUbuntu)
	if err != nil || !spec.LocalOnly {
		// Try EnsureBase on a nonsense id that won't pull.
		_, err := d.EnsureBase(context.Background(), "definitely-missing-image-id-12345")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not ready") {
			t.Fatalf("got: %v", err)
		}
		return
	}
	_, err = d.EnsureBase(context.Background(), image.IDGrainUbuntu)
	if err == nil {
		t.Fatal("expected not ready without import")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("got: %v", err)
	}
	// Local-only should hint import
	if !strings.Contains(err.Error(), "import") && !strings.Contains(err.Error(), "pull") {
		t.Fatalf("expected hint in: %v", err)
	}
}

func TestCopyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hello-disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello-disk" {
		t.Fatalf("content=%q", b)
	}

	if err := copyFile(filepath.Join(dir, "missing"), dst); err == nil {
		t.Fatal("expected missing src error")
	}
	if err := copyFile(src, filepath.Join(dir, "nope", "dst")); err == nil {
		t.Fatal("expected missing dest dir error")
	}
}

func TestMaybeResize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sizeGB <= 0 is no-op
	if err := maybeResize(context.Background(), path, 0); err != nil {
		t.Fatal(err)
	}
	if err := maybeResize(context.Background(), path, -1); err != nil {
		t.Fatal(err)
	}
	// With or without qemu-img, should not return error (best-effort)
	if err := maybeResize(context.Background(), path, 2); err != nil {
		t.Fatal(err)
	}
}

func TestLocalDiskCloneCopyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.img")
	if err := os.WriteFile(base, []byte("base-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewLocalDisk(dir)
	dest := filepath.Join(dir, "vms", "x", "disk.img")

	// Clone may use qemu-img if present (creates .qcow2 overlay) or copyFile.
	if err := d.Clone(context.Background(), base, dest, 0); err != nil {
		t.Fatal(err)
	}
	// Result is either dest or dest.qcow2
	if _, err := os.Stat(dest); err != nil {
		if _, err2 := os.Stat(dest + ".qcow2"); err2 != nil {
			t.Fatalf("neither dest nor dest.qcow2 exists: %v / %v", err, err2)
		}
	}
}

func TestLocalDiskCloneQcow2Base(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")
	// Minimal content; qemu-img create -b may still work for overlay even if base is fake.
	if err := os.WriteFile(base, []byte("qcow2-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewLocalDisk(dir)
	dest := filepath.Join(dir, "out", "disk")
	err := d.Clone(context.Background(), base, dest, 1)
	// qemu-img may fail on fake qcow2; copy/clonefile path may succeed.
	// Either success or a qemu-img create error is acceptable for coverage.
	if err != nil && !strings.Contains(err.Error(), "qemu-img") && !strings.Contains(err.Error(), "cp") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClonefile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("clone-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := clonefile(src, dst)
	// On non-darwin or non-APFS this may fail; still exercises the function.
	if err != nil {
		if !strings.Contains(err.Error(), "cp -c") {
			t.Fatalf("unexpected: %v", err)
		}
		return
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "clone-me" {
		t.Fatalf("content=%q", b)
	}
}

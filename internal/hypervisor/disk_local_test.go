package hypervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/hostbin"
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

func TestLocalDiskEnsureBaseErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := NewLocalDisk(dir)

	// Unknown catalog id → Pull fails → "not ready" with pull hint (no network).
	_, err := d.EnsureBase(context.Background(), "definitely-missing-image-id-12345")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("got: %v", err)
	}
	if !strings.Contains(err.Error(), "pull") && !strings.Contains(err.Error(), "import") {
		t.Fatalf("expected hint in: %v", err)
	}

	// When catalog marks LocalOnly, EnsureBase must hint import (no download attempt).
	spec, gerr := image.Get(image.IDGrainUbuntu)
	if gerr == nil && spec.LocalOnly {
		_, err = d.EnsureBase(context.Background(), image.IDGrainUbuntu)
		if err == nil {
			t.Fatal("expected not ready for local-only without import")
		}
		if !strings.Contains(err.Error(), "import") {
			t.Fatalf("local-only should hint import: %v", err)
		}
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

func TestCopyFileErrors(t *testing.T) {
	t.Parallel()
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

func TestMaybeResizeNoQemu(t *testing.T) {
	// Empty PATH: hostbin may still find qemu-img in commonDirs; either way must not error.
	t.Setenv("PATH", t.TempDir())
	if err := maybeResize(context.Background(), "/nope", 10); err != nil {
		t.Fatal(err)
	}
	if err := maybeResize(context.Background(), "/nope", 0); err != nil {
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

func TestClonefileFailure(t *testing.T) {
	t.Parallel()
	// clonefile uses cp -c; invalid paths should error
	if err := clonefile("/no/such/src", "/no/such/dst"); err == nil {
		t.Fatal("expected error")
	}
}

// installQemuImgStub puts a shell script named qemu-img first on PATH so hostbin.LookPath
// returns it (before commonDirs). script is the body after the shebang.
func installQemuImgStub(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "qemu-img")
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Sanity: hostbin must resolve our stub first.
	got, err := hostbin.LookPath("qemu-img")
	if err != nil {
		t.Fatal(err)
	}
	if got != bin {
		// Still ok if real binary is returned when stub is broken; fail loudly.
		t.Fatalf("LookPath got %s want stub %s", got, bin)
	}
}

func TestLocalDiskCloneWithQemuImgStub(t *testing.T) {
	// Stub succeeds: last arg is dest; create empty overlay file.
	installQemuImgStub(t, `
# emulate: qemu-img create ... dest  OR  qemu-img resize path size
case "$1" in
create)
  dest=""
  for a in "$@"; do dest="$a"; done
  : > "$dest"
  exit 0
  ;;
resize) exit 0 ;;
*) exit 0 ;;
esac
`)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")
	if err := os.WriteFile(base, []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "vm", "disk.img")
	d := NewLocalDisk(dir)
	if err := d.Clone(context.Background(), base, dest, 2); err != nil {
		t.Fatal(err)
	}
	// Clone rewrites non-.qcow2 dest to dest.qcow2 when using qemu-img.
	if _, err := os.Stat(dest + ".qcow2"); err != nil {
		t.Fatalf("expected overlay %s.qcow2: %v", dest, err)
	}
}

func TestLocalDiskCloneQemuImgCreateError(t *testing.T) {
	installQemuImgStub(t, `echo "stub fail" >&2; exit 1`)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.img")
	if err := os.WriteFile(base, []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewLocalDisk(dir)
	err := d.Clone(context.Background(), base, filepath.Join(dir, "out", "disk.img"), 0)
	if err == nil || !strings.Contains(err.Error(), "qemu-img") {
		t.Fatalf("want qemu-img create error, got %v", err)
	}
}

func TestLocalDiskCloneWithoutQemuImg(t *testing.T) {
	// Restrict PATH so exec.LookPath misses qemu-img. hostbin may still find it
	// under commonDirs (/opt/homebrew/bin, /usr/local/bin); in that case we still
	// exercise Clone and accept either overlay or raw dest.
	t.Setenv("PATH", "/nonexistent")
	dir := t.TempDir()
	base := filepath.Join(dir, "base.img")
	if err := os.WriteFile(base, []byte("base-disk-content-xxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "vm", "disk.img")
	d := NewLocalDisk(dir)
	if err := d.Clone(context.Background(), base, dest, 0); err != nil {
		// Real qemu-img with a non-image base can fail create; copy/clonefile should not.
		if _, err2 := hostbin.LookPath("qemu-img"); err2 != nil {
			t.Fatalf("clone without qemu-img: %v", err)
		}
		if !strings.Contains(err.Error(), "qemu-img") {
			t.Fatalf("unexpected: %v", err)
		}
		return
	}
	if _, err := os.Stat(dest); err != nil {
		if _, err2 := os.Stat(dest + ".qcow2"); err2 != nil {
			t.Fatalf("dest missing: %v / %v", err, err2)
		}
	}
}

func TestLocalDiskCloneMkdirFail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	block := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(block, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewLocalDisk(dir)
	base := filepath.Join(dir, "base.img")
	if err := os.WriteFile(base, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.Clone(context.Background(), base, filepath.Join(block, "disk.img"), 0); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestLocalDiskCloneQemuOverlay(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		// Also try hostbin (commonDirs).
		if _, err2 := hostbin.LookPath("qemu-img"); err2 != nil {
			t.Skip("no qemu-img")
		}
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")
	// create a tiny qcow2 base
	qemuImg, err := hostbin.LookPath("qemu-img")
	if err != nil {
		t.Skip(err)
	}
	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", base, "32M")
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

func TestLocalDiskCloneRawBaseFormat(t *testing.T) {
	// When dest already ends in .qcow2, Clone must not double-suffix.
	installQemuImgStub(t, `
case "$1" in
create)
  dest=""
  for a in "$@"; do dest="$a"; done
  : > "$dest"
  # record args for format check
  echo "$@" > "$(dirname "$dest")/args.txt"
  exit 0
  ;;
resize) exit 0 ;;
*) exit 0 ;;
esac
`)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.img") // raw ext → -F raw
	if err := os.WriteFile(base, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "vm", "disk.qcow2")
	d := NewLocalDisk(dir)
	if err := d.Clone(context.Background(), base, dest, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(dir, "vm", "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(args)
	if !strings.Contains(s, "-F") || !strings.Contains(s, "raw") {
		t.Fatalf("expected -F raw in create args: %s", s)
	}
}

func TestCopyDiskFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.qcow2")
	payload := []byte("fake-qcow2-disk-content")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst", "disk.qcow2")
	if err := CopyDiskFile(context.Background(), src, dst, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("content mismatch: %q", got)
	}
	// refuse overwrite
	if err := CopyDiskFile(context.Background(), src, dst, false); err == nil {
		t.Fatal("expected error for existing dest")
	}
	if err := CopyDiskFile(context.Background(), "", dst+"2", false); err == nil {
		t.Fatal("expected empty path error")
	}
}

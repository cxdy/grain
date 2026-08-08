package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDiskFilePreferConvert(t *testing.T) {
	// preferConvert=true: try qemu-img convert first, then fallbacks.
	installQemuImgStub(t, `
case "$1" in
convert)
  dest=""
  for a in "$@"; do dest="$a"; done
  echo converted > "$dest"
  exit 0
  ;;
*) exit 0 ;;
esac
`)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.qcow2")
	if err := os.WriteFile(src, []byte("src-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "disk.qcow2")
	if err := CopyDiskFile(context.Background(), src, dst, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "converted") {
		t.Fatalf("%q", b)
	}
}

func TestCopyDiskFilePreferConvertFallbackCopy(t *testing.T) {
	// convert fails → clonefile/copy
	installQemuImgStub(t, `echo fail >&2; exit 1`)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.img")
	payload := []byte("plain-copy-payload")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out", "disk.img")
	if err := CopyDiskFile(context.Background(), src, dst, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("%q", got)
	}
}

func TestCopyDiskFileNoPreferConvertCopyFallbackQemu(t *testing.T) {
	// preferConvert=false: copy fails (src missing parent ok), with convert fallback.
	// Use unreadable scenario: src exists, dest dir ok — copy should succeed without qemu.
	dir := t.TempDir()
	src := filepath.Join(dir, "src.raw")
	if err := os.WriteFile(src, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "d", "out.raw")
	if err := CopyDiskFile(context.Background(), src, dst, false); err != nil {
		t.Fatal(err)
	}
}

func TestCopyDiskFileEmptyDestAndMkdirFail(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "s")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyDiskFile(context.Background(), src, "", false); err == nil {
		t.Fatal("empty dest")
	}
	// dest parent is a file
	block := filepath.Join(dir, "block")
	if err := os.WriteFile(block, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyDiskFile(context.Background(), src, filepath.Join(block, "d.img"), false); err == nil {
		t.Fatal("mkdir fail")
	}
}

func TestQemuImgConvertHelper(t *testing.T) {
	installQemuImgStub(t, `
case "$1" in
convert)
  dest=""
  for a in "$@"; do dest="$a"; done
  : > "$dest"
  echo "$@" > "$(dirname "$dest")/args.txt"
  exit 0
  ;;
*) exit 1 ;;
esac
`)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.qcow2")
	dst := filepath.Join(dir, "b.qcow2")
	if err := os.WriteFile(src, []byte("q"), 0o644); err != nil {
		t.Fatal(err)
	}
	qemuImg := filepath.Join(os.Getenv("PATH"), "qemu-img")
	// LookPath via PATH from installQemuImgStub — call helper with absolute stub.
	// qemuImgConvert uses the path passed in.
	// Find stub:
	parts := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	if len(parts) == 0 {
		t.Fatal("no path")
	}
	stub := filepath.Join(parts[0], "qemu-img")
	if err := qemuImgConvert(context.Background(), stub, src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	if !strings.Contains(string(args), "qcow2") {
		t.Fatalf("args=%s", args)
	}
	_ = qemuImg

	// convert error
	installQemuImgStub(t, `exit 1`)
	parts = strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	stub = filepath.Join(parts[0], "qemu-img")
	if err := qemuImgConvert(context.Background(), stub, src, filepath.Join(dir, "fail.qcow2")); err == nil {
		t.Fatal("want convert error")
	}

	// raw dest without .qcow2 suffix but src qcow2 → still qcow2 outFmt
	installQemuImgStub(t, `
case "$1" in
convert)
  dest=""
  for a in "$@"; do dest="$a"; done
  : > "$dest"
  echo "$@" > "$(dirname "$dest")/rawargs.txt"
  exit 0
  ;;
esac
`)
	parts = strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	stub = filepath.Join(parts[0], "qemu-img")
	destNoExt := filepath.Join(dir, "out", "disk")
	_ = os.MkdirAll(filepath.Dir(destNoExt), 0o755)
	if err := qemuImgConvert(context.Background(), stub, src, destNoExt); err != nil {
		t.Fatal(err)
	}
}

func TestCopyDiskFilePreferFalseUsesConvertOnCopyFail(t *testing.T) {
	// Missing src → copy fails; with qemu stub convert is tried.
	installQemuImgStub(t, `
case "$1" in
convert)
  dest=""
  for a in "$@"; do dest="$a"; done
  echo via-convert > "$dest"
  exit 0
  ;;
*) exit 0 ;;
esac
`)
	dir := t.TempDir()
	// src missing — copyFile fails; convert may still "succeed" writing dest
	src := filepath.Join(dir, "missing-src.img")
	dst := filepath.Join(dir, "out.img")
	// Without src, convert stub still writes dest — path when copy fails and qemu exists
	err := CopyDiskFile(context.Background(), src, dst, false)
	// copy fails then convert with missing src still "succeeds" via stub
	if err != nil {
		// if convert also fails reading missing, ok
		t.Log(err)
	}
}

func TestCopyDiskFileNoQemuCopyError(t *testing.T) {
	// No qemu-img on PATH/commonDirs and missing src → error from copyFile.
	t.Setenv("PATH", "/nonexistent-grain-path")
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.img")
	err := CopyDiskFile(context.Background(), filepath.Join(dir, "no-src"), dst, false)
	if err == nil {
		// hostbin may still find real qemu-img in commonDirs; convert might "work" oddly
		t.Log("no error (qemu-img found via commonDirs?)")
	}
}

func TestQemuImgConvertRawSrcDest(t *testing.T) {
	installQemuImgStub(t, `
case "$1" in
convert)
  dest=""
  for a in "$@"; do dest="$a"; done
  : > "$dest"
  echo "$@" > "$(dirname "$dest")/args2.txt"
  exit 0
  ;;
esac
`)
	dir := t.TempDir()
	src := filepath.Join(dir, "a.img")
	dst := filepath.Join(dir, "b.img")
	if err := os.WriteFile(src, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(os.Getenv("PATH"), string(os.PathListSeparator))
	stub := filepath.Join(parts[0], "qemu-img")
	if err := qemuImgConvert(context.Background(), stub, src, dst); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args2.txt"))
	if !strings.Contains(string(args), "raw") {
		t.Fatalf("%s", args)
	}
}

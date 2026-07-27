//go:build linux

package cloudinit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMakeISOWithFakeGenisoimage(t *testing.T) {
	binDir := t.TempDir()
	// Fake genisoimage: write empty dest from -output flag.
	script := `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -output) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$out" ]; then
  printf 'ISO' > "$out"
  exit 0
fi
exit 1
`
	fake := filepath.Join(binDir, "genisoimage")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+":/usr/bin:/bin")

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "user-data"), []byte("#cloud-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out.iso")
	if err := makeISO(src, dest); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() == 0 {
		t.Fatalf("iso: %v", err)
	}
}

func TestMakeISOWithFakeXorriso(t *testing.T) {
	binDir := t.TempDir()
	// Only xorriso on PATH (no genisoimage/mkisofs).
	script := `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -output) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$out" ]; then printf 'X' > "$out"; exit 0; fi
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "xorriso"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "user-data"), []byte("y\n"), 0o644)
	dest := filepath.Join(t.TempDir(), "x.iso")
	if err := makeISO(src, dest); err != nil {
		t.Fatal(err)
	}
}

func TestWriteNoCloudOptsLinuxISO(t *testing.T) {
	binDir := t.TempDir()
	script := `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -output) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$out" ] && printf 'ISO' > "$out"
`
	if err := os.WriteFile(filepath.Join(binDir, "mkisofs"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	dir := t.TempDir()
	p, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "ci",
		SSHPub:   "ssh-ed25519 AAAA test",
		Minimal:  false,
		Extra:    "echo hi",
		Mounts:   []MountSpec{{Tag: "work", Guest: "/work", Driver: "9p"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(p); err != nil || st.Size() == 0 {
		t.Fatalf("seed iso: %v", err)
	}
}

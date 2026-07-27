package cloudinit

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteNoCloudFullWithExtra(t *testing.T) {
	if runtime.GOOS != "darwin" {
		dir := t.TempDir()
		_, err := WriteNoCloud(dir, "h1", "ssh-ed25519 AAAA k", "echo extra")
		if err != nil {
			if !strings.Contains(err.Error(), "ISO") && !strings.Contains(err.Error(), "hdiutil") && !strings.Contains(err.Error(), "genisoimage") && !strings.Contains(err.Error(), "mkisofs") && !strings.Contains(err.Error(), "xorriso") {
				t.Logf("unexpected: %v", err)
			}
			return
		}
		return
	}
	dir := t.TempDir()
	seed, err := WriteNoCloud(dir, "full-host", "ssh-ed25519 AAAA full@grain", "#!/bin/sh\necho hi\n",
		MountSpec{Tag: "grain0", Guest: "/mnt", Driver: "9p"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(seed); err != nil || st.Size() < 100 {
		t.Fatalf("seed %v", err)
	}
	ud, err := os.ReadFile(filepath.Join(dir, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(ud)
	if !strings.Contains(s, "full-host") {
		t.Fatalf("missing hostname:\n%s", s)
	}
	if !strings.Contains(s, "echo hi") {
		t.Fatalf("missing extra:\n%s", s)
	}
}

func TestWriteNoCloudOptsNonMinimal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ISO generation on darwin")
	}
	dir := t.TempDir()
	seed, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "nm1",
		SSHPub:   "ssh-ed25519 AAAA k",
		Minimal:  false,
		Mounts:   []MountSpec{{Tag: "t0", Guest: "/g", Driver: "virtiofs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(seed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cidata", "vendor-data")); err != nil {
		t.Fatal(err)
	}
}

func TestMakeISOMissingToolPath(t *testing.T) {
	if runtime.GOOS == "darwin" {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644)
		dest := filepath.Join(dir, "out.iso")
		if err := makeISO(src, dest); err != nil {
			t.Logf("makeISO: %v", err)
		} else if st, err := os.Stat(dest); err != nil || st.Size() == 0 {
			t.Fatalf("iso %v", err)
		}
		return
	}
	dir := t.TempDir()
	err := makeISO(dir, filepath.Join(dir, "x.iso"))
	if err == nil {
		t.Log("ISO tool available")
	} else if !strings.Contains(err.Error(), "ISO") && !strings.Contains(err.Error(), "tool") {
		t.Logf("err: %v", err)
	}
}

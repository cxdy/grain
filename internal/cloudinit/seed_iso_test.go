package cloudinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteNoCloudOptsFullISO(t *testing.T) {
	if runtime.GOOS != "darwin" {
		found := false
		for _, bin := range []string{"genisoimage", "mkisofs", "xorriso"} {
			if _, err := exec.LookPath(bin); err == nil {
				found = true
				break
			}
		}
		if !found {
			t.Skip("no ISO tool")
		}
	}
	dir := t.TempDir()
	p, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "h",
		SSHPub:   "ssh-ed25519 AAAA test",
		Minimal:  true,
		Mounts:   []MountSpec{{Tag: "work", Guest: "/work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() == 0 {
		t.Fatalf("iso %v", err)
	}
}

func TestMakeISOMissingTool(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin always has hdiutil")
	}
	t.Setenv("PATH", "/nonexistent")
	err := makeISO(t.TempDir(), filepath.Join(t.TempDir(), "x.iso"))
	if err == nil {
		t.Fatal("expected no ISO tool")
	}
}

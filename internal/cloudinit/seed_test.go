package cloudinit_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/cloudinit"
)

func TestWriteNoCloud(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ISO generation tested on darwin (hdiutil)")
	}
	dir := t.TempDir()
	seed, err := cloudinit.WriteNoCloud(dir, "sbox-1", "ssh-ed25519 AAAA test@grain", "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(seed)
	if err != nil || st.Size() < 100 {
		t.Fatalf("seed %v size", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cidata", "user-data")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteNoCloudOpts_MinimalUserData(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ISO generation tested on darwin (hdiutil)")
	}
	dir := t.TempDir()
	key := "ssh-ed25519 AAAA minimal@grain"
	seed, err := cloudinit.WriteNoCloudOpts(dir, cloudinit.SeedOpts{
		Hostname: "fast-1",
		SSHPub:   key,
		Minimal:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(seed)
	if err != nil || st.Size() < 100 {
		t.Fatalf("seed %v size", err)
	}
	ud, err := os.ReadFile(filepath.Join(dir, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(ud)
	if !strings.HasPrefix(s, "#cloud-config\n") {
		t.Fatalf("user-data header:\n%s", s)
	}
	if !strings.Contains(s, "hostname: fast-1") {
		t.Fatalf("missing hostname:\n%s", s)
	}
	if !strings.Contains(s, key) {
		t.Fatalf("missing key:\n%s", s)
	}
	if !strings.Contains(s, "userdata-ran") {
		t.Fatalf("missing userdata-ran:\n%s", s)
	}
	if !strings.Contains(s, "package_update: false") {
		t.Fatalf("expected package_update false:\n%s", s)
	}
	// Full path must not force package_update.
	dir2 := t.TempDir()
	if _, err := cloudinit.WriteNoCloud(dir2, "full-1", key, ""); err != nil {
		t.Fatal(err)
	}
	full, err := os.ReadFile(filepath.Join(dir2, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(full), "package_update") {
		t.Fatalf("full seed should not set package_update:\n%s", full)
	}
}

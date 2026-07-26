package cloudinit_test

import (
	"os"
	"path/filepath"
	"runtime"
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

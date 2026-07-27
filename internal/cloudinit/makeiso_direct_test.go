package cloudinit

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMakeISODirect(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "user-data"), []byte("#cloud-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out.iso")
	if err := makeISO(src, dest); err != nil {
		if runtime.GOOS != "darwin" {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() == 0 {
		t.Fatalf("iso: %v", err)
	}
}

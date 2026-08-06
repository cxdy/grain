package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/hostbin"
)

func TestDiskNeedsGrow(t *testing.T) {
	if _, err := hostbin.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not available")
	}
	dir := t.TempDir()
	img := filepath.Join(dir, "base.qcow2")
	// 1G sparse qcow2
	cmd := exec.Command("qemu-img", "create", "-f", "qcow2", img, "1G")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create: %v %s", err, out)
	}
	if diskNeedsGrow(img, 1) {
		t.Fatal("1G base should not need grow for 1G target")
	}
	if !diskNeedsGrow(img, 2) {
		t.Fatal("1G base should need grow for 2G target")
	}
	if diskNeedsGrow(img, 0) {
		t.Fatal("size 0 should not need grow")
	}
	if diskNeedsGrow("", 5) {
		t.Fatal("empty path should not need grow")
	}
	_ = os.Remove(img)
}

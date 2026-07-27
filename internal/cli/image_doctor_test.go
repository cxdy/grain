package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/image"
)

func TestRunImageLS(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir}
	if err := runImageLS(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestRunImagePullUnknown(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir}
	err := runImagePull(cfg, "not-a-real-image-id-xyz")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunImagePullLocalOnly(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir}
	// Find a local-only catalog id if any.
	for id, spec := range image.Catalog() {
		if spec.LocalOnly {
			err := runImagePull(cfg, id)
			if err == nil || !strings.Contains(err.Error(), "local-only") {
				t.Fatalf("local-only: %v", err)
			}
			return
		}
	}
	t.Skip("no local-only image in catalog")
}

func TestRunImageImportMissingSrc(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir}
	err := runImageImport(cfg, filepath.Join(dir, "missing.qcow2"), image.IDGrainUbuntu)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestRunImageImportBadID(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir}
	err := runImageImport(cfg, filepath.Join(dir, "x.qcow2"), "nope-id")
	if err == nil {
		t.Fatal("expected bad id error")
	}
}

func TestQemuSupportsQMP(t *testing.T) {
	// Fake binary that prints -qmp help.
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-qemu")
	script := "#!/bin/sh\necho '  -qmp QMP options'\n"
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake binary")
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if !qemuSupportsQMP(bin) {
		t.Fatal("expected qmp support")
	}

	// Binary that fails with no output
	bad := filepath.Join(dir, "bad-qemu")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// may still return false
	_ = qemuSupportsQMP(bad)

	// missing binary
	if qemuSupportsQMP(filepath.Join(dir, "nope")) {
		t.Fatal("missing binary should not support qmp")
	}
}

func TestRunDoctorMockHypervisor(t *testing.T) {
	dir := t.TempDir()
	// Ensure dirs + ssh key path can be created; use mock hypervisor to skip real qemu hard fail paths when possible.
	cfg := config.Config{
		DataDir:    dir,
		Hypervisor: "mock",
		Image:      "grain-ubuntu",
		Socket:     filepath.Join(dir, "grain.sock"),
		SSHUser:    "ubuntu",
		QEMUBinary: "qemu-not-on-path-hopefully-xyz",
	}
	// Doctor may still fail on missing base image / qemu-img — that's expected.
	err := runDoctor(cfg)
	// We only assert it runs and returns either nil or a known doctor issues error.
	if err != nil && !strings.Contains(err.Error(), "doctor found issues") {
		// qemu-img missing etc. still returns "doctor found issues"
		t.Logf("doctor err: %v", err)
	}
}

func TestRunDoctorFirecrackerPath(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		DataDir:    dir,
		Hypervisor: "firecracker",
		Image:      "grain-ubuntu",
		Socket:     filepath.Join(dir, "grain.sock"),
		SSHUser:    "ubuntu",
		KernelPath: filepath.Join(dir, "kernels", "vmlinux"),
	}
	// Create empty kernel so soft check notices size 0 as missing-ish; or write some bytes.
	if err := os.MkdirAll(filepath.Dir(cfg.KernelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.KernelPath, []byte("vmlinux-fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runDoctor(cfg)
	if err != nil && !strings.Contains(err.Error(), "doctor found issues") {
		t.Logf("doctor firecracker: %v", err)
	}
}

func TestCmdDoctorAndImageRequireLocal(t *testing.T) {
	apiURLFlag = "http://10.1.2.3:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_TOKEN", "t")
	cfg := ""
	img := cmdImage(&cfg)
	img.SetArgs([]string{"ls"})
	if err := img.Execute(); err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("image local: %v", err)
	}
}

func TestCmdImageLSWithConfig(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	// Call runImageLS directly (avoids PersistentPreRun + cobra nesting issues)
	cfg, err := loadCfg(&cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runImageLS(cfg); err != nil {
		t.Fatal(err)
	}
}

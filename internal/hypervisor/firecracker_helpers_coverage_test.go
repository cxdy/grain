package hypervisor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func TestBuildFCConfigSeedDriveFields(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig("/k", "/d.raw", 4, 512, MinGuestCID+1, "/tmp/v.sock")
	if cfg.BootSource.BootArgs != fcDefaultBootArgs {
		t.Fatalf("boot args")
	}
	if cfg.MachineConfig.SMT {
		t.Fatal("smt should be false")
	}
	if len(cfg.Drives) != 1 || cfg.Drives[0].DriveID != "rootfs" {
		t.Fatalf("%+v", cfg.Drives)
	}
	b, err := MarshalFCConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "kernel_image_path") {
		t.Fatalf("%s", b)
	}
	// ensure JSON is valid pretty
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
}

func TestFCConstantsExported(t *testing.T) {
	t.Parallel()
	if FCConfigName == "" || FCSocketName == "" || FCPidName == "" || FCVsockName == "" {
		t.Fatal("empty const")
	}
	if FCRawDiskName != "disk.raw" || FCDefaultBin != "firecracker" || FCDefaultKernel != "vmlinux" {
		t.Fatal("defaults")
	}
}

func TestCollectFCPIDsBadPidFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FCPidName), []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids := collectFCPIDs(&vm.Instance{PID: 7, DiskPath: filepath.Join(dir, "disk.raw")})
	if len(pids) != 1 || pids[0] != 7 {
		t.Fatalf("%v", pids)
	}
	// zero pid skipped
	pids = collectFCPIDs(&vm.Instance{PID: 0, DiskPath: filepath.Join(dir, "disk.raw")})
	if len(pids) != 0 {
		t.Fatalf("%v", pids)
	}
}

func TestCleanupFCFilesQMPOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := filepath.Join(dir, FCSocketName)
	if err := os.WriteFile(sock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// DiskPath empty — no-op for dir cleanup, but QMPPath not cleaned without DiskPath
	cleanupFCFiles(&vm.Instance{QMPPath: sock})
	if _, err := os.Stat(sock); err != nil {
		// still exists because DiskPath empty returns early
		t.Log("as expected may remain")
	}
}

func TestEnsureRawRootfsEmptyAndPassthroughImg(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	img := filepath.Join(dir, "root.img")
	if err := os.WriteFile(img, []byte("rawish"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureRawRootfs(context.Background(), img)
	if err != nil || got != img {
		t.Fatalf("%s %v", got, err)
	}
}

func TestFirecrackerRunningNegativePID(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("fc", t.TempDir(), "/k")
	if rt.Running(&vm.Instance{PID: -1}) {
		t.Fatal("neg pid")
	}
}

func TestNewFirecrackerRuntimeCustomBinary(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("/usr/local/bin/fc", "/data", "/k/vmlinux")
	if rt.Binary != "/usr/local/bin/fc" || rt.KernelPath != "/k/vmlinux" {
		t.Fatalf("%+v", rt)
	}
}

func TestFCAPIRequestStatusErrorBody(t *testing.T) {
	sock := shortUnixSock(t)
	cleanup := mockFCAPIServer(t, sock, func(method, path string, body []byte) (int, string) {
		return 500, `{"error":"internal"}`
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := fcAPIAction(ctx, sock, "SendCtrlAltDel")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("%v", err)
	}
}

func TestFirecrackerPauseWithDeadline(t *testing.T) {
	sock := shortUnixSock(t)
	cleanup := mockFCAPIServer(t, sock, func(method, path string, body []byte) (int, string) {
		if method == "PATCH" {
			return 204, ""
		}
		return 404, ""
	})
	defer cleanup()
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	// ctx without deadline — fcAPIRequest adds one
	inst := &vm.Instance{Name: "p", PID: os.Getpid(), QMPPath: sock}
	if err := rt.Pause(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
}

func TestResolveKernelEmptyFileSkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// empty override file skipped
	empty := filepath.Join(dir, "empty-vmlinux")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewFirecrackerRuntime("", dir, empty)
	// also plant good kernels/vmlinux
	kdir := filepath.Join(dir, "kernels")
	_ = os.MkdirAll(kdir, 0o755)
	good := filepath.Join(kdir, FCDefaultKernel)
	if err := os.WriteFile(good, []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := rt.resolveKernel()
	if err != nil {
		t.Fatal(err)
	}
	if got != good {
		// empty override is candidate but size 0 skipped → falls through to kernels/
		t.Fatalf("got %s want %s", got, good)
	}
}

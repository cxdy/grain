package hypervisor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/hostbin"
	"github.com/cxdy/grain/internal/vm"
)

// shortUnixSock returns a short path under /tmp (macOS unix socket path limit is ~104 bytes).
func shortUnixSock(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "gfc-*.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	_ = f.Close()
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func mockFCAPIServer(t *testing.T, sock string, handler func(method, path string, body []byte) (int, string)) func() {
	t.Helper()
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0)
		if r.Body != nil {
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			body = buf[:n]
		}
		code, resp := 200, `{}`
		if handler != nil {
			code, resp = handler(r.Method, r.URL.Path, body)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(resp))
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(ln)
	}()
	return func() {
		_ = srv.Close()
		_ = ln.Close()
		wg.Wait()
	}
}

func TestBuildFCConfigJSON(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig(
		"/kernels/vmlinux",
		"/vms/sbox/disk.raw",
		2,
		2048,
		42,
		"/vms/sbox/fc-vsock.sock",
		nil,
	)
	b, err := MarshalFCConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// no network-interfaces without plan
	if strings.Contains(string(b), "network-interfaces") {
		t.Fatalf("unexpected network-interfaces: %s", b)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("json: %v\n%s", err, b)
	}

	boot, ok := raw["boot-source"].(map[string]any)
	if !ok {
		t.Fatalf("missing boot-source: %s", b)
	}
	if boot["kernel_image_path"] != "/kernels/vmlinux" {
		t.Fatalf("kernel path: %v", boot["kernel_image_path"])
	}
	if boot["boot_args"] == "" {
		t.Fatal("expected boot_args")
	}

	drives, ok := raw["drives"].([]any)
	if !ok || len(drives) < 1 {
		t.Fatalf("drives: %v", raw["drives"])
	}
	d0 := drives[0].(map[string]any)
	if d0["path_on_host"] != "/vms/sbox/disk.raw" {
		t.Fatalf("rootfs path: %v", d0["path_on_host"])
	}
	if d0["is_root_device"] != true {
		t.Fatal("expected is_root_device")
	}

	mc := raw["machine-config"].(map[string]any)
	if int(mc["vcpu_count"].(float64)) != 2 {
		t.Fatalf("vcpu_count: %v", mc["vcpu_count"])
	}
	if int(mc["mem_size_mib"].(float64)) != 2048 {
		t.Fatalf("mem_size_mib: %v", mc["mem_size_mib"])
	}

	vs, ok := raw["vsock"].(map[string]any)
	if !ok {
		t.Fatalf("missing vsock: %s", b)
	}
	if int(vs["guest_cid"].(float64)) != 42 {
		t.Fatalf("guest_cid: %v", vs["guest_cid"])
	}
	if vs["uds_path"] != "/vms/sbox/fc-vsock.sock" {
		t.Fatalf("uds_path: %v", vs["uds_path"])
	}
}

func TestBuildFCConfigWithNet(t *testing.T) {
	t.Parallel()
	plan := PlanFCNet("net-test")
	cfg := BuildFCConfig("/k", "/d", 1, 512, MinGuestCID, "/v.sock", &plan)
	b, err := MarshalFCConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	nis, ok := raw["network-interfaces"].([]any)
	if !ok || len(nis) != 1 {
		t.Fatalf("network-interfaces: %s", b)
	}
	ni := nis[0].(map[string]any)
	if ni["host_dev_name"] != plan.TapName {
		t.Fatalf("tap %v want %s", ni["host_dev_name"], plan.TapName)
	}
	if ni["guest_mac"] != plan.GuestMAC {
		t.Fatalf("mac %v", ni["guest_mac"])
	}
}

func TestBuildFCConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig("/k", "/d", 0, 0, MinGuestCID, "/v.sock", nil)
	if cfg.MachineConfig.VCPUCount != 1 {
		t.Fatalf("cpus=%d", cfg.MachineConfig.VCPUCount)
	}
	if cfg.MachineConfig.MemSizeMiB != 128 {
		t.Fatalf("mem=%d", cfg.MachineConfig.MemSizeMiB)
	}
	if cfg.Vsock == nil || cfg.Vsock.GuestCID != MinGuestCID {
		t.Fatalf("vsock=%+v", cfg.Vsock)
	}
	// Empty vsock UDS omits vsock even with good CID
	cfg2 := BuildFCConfig("/k", "/d", 2, 256, 10, "", nil)
	if cfg2.Vsock != nil {
		t.Fatalf("expected nil vsock: %+v", cfg2.Vsock)
	}
}

func TestBuildFCConfigOmitsVsockWhenCIDLow(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig("/k", "/d", 1, 128, 0, "/v.sock", nil)
	b, err := MarshalFCConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["vsock"]; ok {
		t.Fatalf("vsock should be omitted when guest_cid < MinGuestCID: %s", b)
	}
}

func TestBuildFCConfigSeedDriveFields(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig("/k", "/d.raw", 4, 512, MinGuestCID+1, "/tmp/v.sock", nil)
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

func TestNewFirecrackerRuntimeDefaults(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("", "/data", "")
	if rt.Binary != FCDefaultBin {
		t.Fatalf("binary %q", rt.Binary)
	}
	if rt.DataDir != "/data" {
		t.Fatalf("datadir %q", rt.DataDir)
	}
}

func TestNewFirecrackerRuntimeCustomBinary(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("/usr/local/bin/fc", "/data", "/k/vmlinux")
	if rt.Binary != "/usr/local/bin/fc" || rt.KernelPath != "/k/vmlinux" {
		t.Fatalf("%+v", rt)
	}
}

func TestFirecrackerStartRequiresLinux(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "linux" {
		t.Skip("linux can attempt real Start; this test is for non-linux failure path")
	}
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	inst := &vm.Instance{Name: "fc-test", CPUs: 1, MemoryMB: 256}
	err := rt.Start(context.Background(), inst, filepath.Join(t.TempDir(), "disk.raw"))
	if err == nil {
		t.Fatal("expected error on non-linux")
	}
	if !strings.Contains(err.Error(), "firecracker requires linux") {
		t.Fatalf("want linux error, got: %v", err)
	}
}

func TestFirecrackerStartMissingBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("binary check runs after OS check")
	}
	rt := NewFirecrackerRuntime("grain-nonexistent-firecracker-bin", t.TempDir(), "")
	// Provide a fake kernel so we fail on binary, not kernel.
	kdir := filepath.Join(rt.DataDir, "kernels")
	_ = os.MkdirAll(kdir, 0o755)
	kpath := filepath.Join(kdir, "vmlinux")
	if err := os.WriteFile(kpath, []byte("not-a-real-kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt.KernelPath = kpath

	disk := filepath.Join(t.TempDir(), "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{Name: "fc-miss", CPUs: 1, MemoryMB: 256}
	err := rt.Start(context.Background(), inst, disk)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found, got: %v", err)
	}
}

func TestFirecrackerSaveVMUnsupported(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	err := rt.SaveVM(context.Background(), &vm.Instance{Name: "x", PID: 1}, "tag")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("got %v", err)
	}
}

func TestFirecrackerResolveKernel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rt := NewFirecrackerRuntime("", dir, "")
	_, err := rt.resolveKernel()
	if err == nil {
		t.Fatal("expected missing kernel")
	}

	kdir := filepath.Join(dir, "kernels")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	kpath := filepath.Join(kdir, "vmlinux")
	if err := os.WriteFile(kpath, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := rt.resolveKernel()
	if err != nil {
		t.Fatal(err)
	}
	if got != kpath {
		t.Fatalf("got %s want %s", got, kpath)
	}

	// Explicit override wins.
	override := filepath.Join(dir, "custom-vmlinux")
	if err := os.WriteFile(override, []byte("k2"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt.KernelPath = override
	got, err = rt.resolveKernel()
	if err != nil {
		t.Fatal(err)
	}
	if got != override {
		t.Fatalf("got %s want %s", got, override)
	}
}

func TestFirecrackerResolveKernelAltPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// kernel/vmlinux alternate path
	kdir := filepath.Join(dir, "kernel")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	kpath := filepath.Join(kdir, FCDefaultKernel)
	if err := os.WriteFile(kpath, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewFirecrackerRuntime("", dir, "")
	got, err := rt.resolveKernel()
	if err != nil {
		t.Fatal(err)
	}
	if got != kpath {
		t.Fatalf("got %s want %s", got, kpath)
	}

	// DataDir root vmlinux
	dir2 := t.TempDir()
	rootK := filepath.Join(dir2, FCDefaultKernel)
	if err := os.WriteFile(rootK, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt2 := NewFirecrackerRuntime("", dir2, "")
	got, err = rt2.resolveKernel()
	if err != nil {
		t.Fatal(err)
	}
	if got != rootK {
		t.Fatalf("got %s", got)
	}

	// Empty size skipped
	dir3 := t.TempDir()
	empty := filepath.Join(dir3, FCDefaultKernel)
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	rt3 := NewFirecrackerRuntime("", dir3, "")
	if _, err := rt3.resolveKernel(); err == nil {
		t.Fatal("empty kernel should fail")
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

func TestEnsureRawRootfsPassthrough(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	raw := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(raw, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureRawRootfs(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("got %s want %s", got, raw)
	}
}

func TestEnsureRawRootfsErrors(t *testing.T) {
	t.Parallel()
	if _, err := ensureRawRootfs(context.Background(), ""); err == nil {
		t.Fatal("empty path")
	}
	if _, err := ensureRawRootfs(context.Background(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing")
	}
	dir := t.TempDir()
	if _, err := ensureRawRootfs(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("dir: %v", err)
	}
}

func TestEnsureRawRootfsReuseConverted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	qcow := filepath.Join(dir, "disk.qcow2")
	raw := filepath.Join(dir, FCRawDiskName)
	// Write qcow first, then raw newer — should reuse without convert.
	if err := os.WriteFile(qcow, []byte("qcow"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure raw mtime is not before qcow
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(raw, []byte("already-raw-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureRawRootfs(context.Background(), qcow)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("got %s want %s", got, raw)
	}
}

func TestEnsureRawRootfsQcow2NeedsQemuImgOrRefuse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	qcow := filepath.Join(dir, "disk.qcow2")
	if err := os.WriteFile(qcow, []byte("qcow2-not-real"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without a valid qcow2, qemu-img convert will fail if present; either
	// missing qemu-img or convert failure is acceptable for this unit test.
	_, err := ensureRawRootfs(context.Background(), qcow)
	if err == nil {
		// Convert succeeded only if qemu-img accepted the fake file (unlikely).
		// If it did, raw should exist.
		if _, stErr := os.Stat(filepath.Join(dir, FCRawDiskName)); stErr != nil {
			t.Fatal("expected raw disk after successful convert")
		}
		return
	}
	msg := err.Error()
	if !strings.Contains(msg, "raw") && !strings.Contains(msg, "qemu-img") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRawRootfsConvertWithStub(t *testing.T) {
	// Stub qemu-img convert: write dest path (last arg) with payload.
	dir := t.TempDir()
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "qemu-img")
	script := `#!/bin/sh
# convert -O raw src dest
dest=""
for a in "$@"; do dest="$a"; done
echo converted > "$dest"
exit 0
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if p, err := hostbin.LookPath("qemu-img"); err != nil || p != stub {
		t.Fatalf("stub not first on PATH: %s %v", p, err)
	}

	qcow := filepath.Join(dir, "disk.qcow2")
	if err := os.WriteFile(qcow, []byte("qcow-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureRawRootfs(context.Background(), qcow)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, FCRawDiskName)
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	b, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "converted") {
		t.Fatalf("content=%q", b)
	}
}

func TestEnsureRawRootfsConvertFailure(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "qemu-img")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := t.TempDir()
	qcow := filepath.Join(dir, "disk.qcow2")
	if err := os.WriteFile(qcow, []byte("qcow"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ensureRawRootfs(context.Background(), qcow)
	if err == nil || !strings.Contains(err.Error(), "qemu-img") {
		t.Fatalf("want convert error, got %v", err)
	}
}

func TestFCAPISock(t *testing.T) {
	t.Parallel()
	if fcAPISock(&vm.Instance{}) != "" {
		t.Fatal("empty instance")
	}
	if got := fcAPISock(&vm.Instance{QMPPath: "/tmp/fc.sock"}); got != "/tmp/fc.sock" {
		t.Fatalf("got %s", got)
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, FCSocketName)
	if err := os.WriteFile(sock, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	got := fcAPISock(&vm.Instance{DiskPath: filepath.Join(dir, "disk.raw")})
	if got != sock {
		t.Fatalf("got %s want %s", got, sock)
	}
	// Disk path without socket file
	dir2 := t.TempDir()
	if got := fcAPISock(&vm.Instance{DiskPath: filepath.Join(dir2, "disk.raw")}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestCollectFCPIDs(t *testing.T) {
	t.Parallel()
	if pids := collectFCPIDs(&vm.Instance{}); len(pids) != 0 {
		t.Fatalf("%v", pids)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FCPidName), []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids := collectFCPIDs(&vm.Instance{PID: 99, DiskPath: filepath.Join(dir, "disk.raw")})
	if len(pids) != 2 {
		t.Fatalf("pids=%v", pids)
	}
	// Dedup
	if err := os.WriteFile(filepath.Join(dir, FCPidName), []byte("99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids = collectFCPIDs(&vm.Instance{PID: 99, DiskPath: filepath.Join(dir, "disk.raw")})
	if len(pids) != 1 || pids[0] != 99 {
		t.Fatalf("pids=%v", pids)
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

func TestCleanupFCFiles(t *testing.T) {
	t.Parallel()
	cleanupFCFiles(&vm.Instance{}) // no-op

	dir := t.TempDir()
	for _, name := range []string{FCPidName, FCSocketName, FCVsockName} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sock := filepath.Join(dir, FCSocketName)
	cleanupFCFiles(&vm.Instance{
		DiskPath: filepath.Join(dir, "disk.raw"),
		QMPPath:  sock,
	})
	for _, name := range []string{FCPidName, FCSocketName, FCVsockName} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed", name)
		}
	}
}

func TestCleanupFCFilesQMPOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := filepath.Join(dir, FCSocketName)
	if err := os.WriteFile(sock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// DiskPath empty — early return; QMPPath not cleaned
	cleanupFCFiles(&vm.Instance{QMPPath: sock})
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("sock should remain without DiskPath: %v", err)
	}
}

func TestFCAPIRequestSuccessAndErrors(t *testing.T) {
	t.Parallel()
	if err := fcAPIRequest(context.Background(), "", http.MethodPut, "/actions", nil); err == nil {
		t.Fatal("empty sock")
	}

	dir := t.TempDir()
	sock := shortUnixSock(t)
	var seen []string
	cleanup := mockFCAPIServer(t, sock, func(method, path string, body []byte) (int, string) {
		seen = append(seen, method+" "+path)
		if path == "/actions" {
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			if m["action_type"] != "SendCtrlAltDel" {
				return 400, `{"error":"bad action"}`
			}
			return 204, ""
		}
		if path == "/vm" {
			return 204, ""
		}
		return 404, "nope"
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := fcAPIAction(ctx, sock, "SendCtrlAltDel"); err != nil {
		t.Fatal(err)
	}
	if err := fcAPIPatchVM(ctx, sock, "Paused"); err != nil {
		t.Fatal(err)
	}
	if err := fcAPIRequest(ctx, sock, http.MethodGet, "/missing", nil); err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("404: %v", err)
	}

	// Bad socket path
	if err := fcAPIRequest(ctx, filepath.Join(dir, "nope.sock"), http.MethodPut, "/actions", []byte(`{}`)); err == nil {
		t.Fatal("expected dial error")
	}
	_ = seen
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

func TestFirecrackerPauseResumeNotRunning(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	ctx := context.Background()
	inst := &vm.Instance{Name: "fc", PID: 0}
	if err := rt.Pause(ctx, inst); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("pause: %v", err)
	}
	if err := rt.Resume(ctx, inst); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("resume: %v", err)
	}

	// Running (self) but no API socket
	alive := &vm.Instance{Name: "fc2", PID: os.Getpid()}
	if err := rt.Pause(ctx, alive); err == nil || !strings.Contains(err.Error(), "API socket") {
		t.Fatalf("pause no sock: %v", err)
	}
	if err := rt.Resume(ctx, alive); err == nil || !strings.Contains(err.Error(), "API socket") {
		t.Fatalf("resume no sock: %v", err)
	}
}

func TestFirecrackerPauseResumeWithAPI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := shortUnixSock(t)
	cleanup := mockFCAPIServer(t, sock, func(method, path string, body []byte) (int, string) {
		if method == http.MethodPatch && path == "/vm" {
			return 204, ""
		}
		return 400, "bad"
	})
	defer cleanup()

	rt := NewFirecrackerRuntime("", dir, "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	inst := &vm.Instance{Name: "fc-api", PID: os.Getpid(), QMPPath: sock}
	if err := rt.Pause(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if err := rt.Resume(ctx, inst); err != nil {
		t.Fatal(err)
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

func TestFirecrackerRunningAndStop(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	if rt.Running(&vm.Instance{PID: 0}) {
		t.Fatal("pid 0")
	}
	if !rt.Running(&vm.Instance{PID: os.Getpid()}) {
		t.Fatal("self")
	}

	dir := t.TempDir()
	sock := shortUnixSock(t)
	// API action succeeds; process is dead PID so graceful exit is quick.
	cleanup := mockFCAPIServer(t, sock, func(method, path string, body []byte) (int, string) {
		return 204, ""
	})
	defer cleanup()

	for _, name := range []string{FCPidName, FCVsockName} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	inst := &vm.Instance{
		Name:     "fc-stop",
		PID:      999999997,
		QMPPath:  sock,
		DiskPath: filepath.Join(dir, "disk.raw"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rt.Stop(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusStopped || inst.PID != 0 {
		t.Fatalf("status=%s pid=%d", inst.Status, inst.PID)
	}
}

func TestFirecrackerStopHardKillNoAPI(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	inst := &vm.Instance{
		Name:     "fc-hard",
		PID:      999999996,
		DiskPath: filepath.Join(t.TempDir(), "disk.raw"),
	}
	if err := rt.Stop(context.Background(), inst); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusStopped {
		t.Fatalf("status=%s", inst.Status)
	}
}

func TestFirecrackerRunningNegativePID(t *testing.T) {
	t.Parallel()
	rt := NewFirecrackerRuntime("fc", t.TempDir(), "/k")
	if rt.Running(&vm.Instance{PID: -1}) {
		t.Fatal("neg pid")
	}
}

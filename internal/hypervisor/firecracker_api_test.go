package hypervisor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestBuildFCConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := BuildFCConfig("/k", "/d", 0, 0, MinGuestCID, "/v.sock")
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
	cfg2 := BuildFCConfig("/k", "/d", 2, 256, 10, "")
	if cfg2.Vsock != nil {
		t.Fatalf("expected nil vsock: %+v", cfg2.Vsock)
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

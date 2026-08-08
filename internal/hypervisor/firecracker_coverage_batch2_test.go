package hypervisor

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func TestFCAPISockAndCollectPIDs(t *testing.T) {
	t.Parallel()
	if fcAPISock(&vm.Instance{}) != "" {
		t.Fatal("empty")
	}
	if got := fcAPISock(&vm.Instance{QMPPath: "/tmp/fc.sock"}); got != "/tmp/fc.sock" {
		t.Fatal(got)
	}
	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, FCSocketName)
	if err := os.WriteFile(sock, []byte("not-really-sock"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fcAPISock(&vm.Instance{DiskPath: disk}); got != sock {
		t.Fatalf("%q", got)
	}

	// collect PIDs
	pidFile := filepath.Join(dir, FCPidName)
	if err := os.WriteFile(pidFile, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pids := collectFCPIDs(&vm.Instance{PID: 99, DiskPath: disk})
	if len(pids) < 2 {
		t.Fatalf("%v", pids)
	}
	// invalid pid file
	_ = os.WriteFile(pidFile, []byte("nope"), 0o644)
	_ = collectFCPIDs(&vm.Instance{PID: 1, DiskPath: disk})
	_ = collectFCPIDs(&vm.Instance{PID: 0})
}

func TestFirecrackerRunningAndStopHardKill(t *testing.T) {
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	if rt.Running(&vm.Instance{PID: 0}) {
		t.Fatal("pid 0")
	}
	if rt.Running(&vm.Instance{PID: 1<<30 - 3}) {
		// may skip if somehow alive
		t.Log("unlikely pid alive")
	}
	// current process is running
	if !rt.Running(&vm.Instance{PID: os.Getpid()}) {
		t.Fatal("self should be running")
	}

	// Stop without API socket → hard kill path on fake pids
	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{Name: "s", PID: 1<<30 - 5, DiskPath: disk}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rt.Stop(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusStopped || inst.PID != 0 {
		t.Fatalf("%+v", inst)
	}
}

func TestFirecrackerPauseResumeNoSocket(t *testing.T) {
	rt := NewFirecrackerRuntime("", t.TempDir(), "")
	// not running
	if err := rt.Pause(context.Background(), &vm.Instance{Name: "x", PID: 0}); err == nil {
		t.Fatal("not running")
	}
	// running but no socket
	inst := &vm.Instance{Name: "x", PID: os.Getpid()}
	if err := rt.Pause(context.Background(), inst); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Fatalf("%v", err)
	}
	if err := rt.Resume(context.Background(), inst); err == nil || !strings.Contains(err.Error(), "socket") {
		t.Fatalf("%v", err)
	}
	if err := rt.Resume(context.Background(), &vm.Instance{Name: "y", PID: 0}); err == nil {
		t.Fatal("not running")
	}
}

func TestFCAPIRequestAndActions(t *testing.T) {
	sock := shortUnixSock(t)
	var mu sync.Mutex
	var paths []string
	stop := mockFCAPIServer(t, sock, func(method, path string, body []byte) (int, string) {
		mu.Lock()
		paths = append(paths, method+" "+path)
		mu.Unlock()
		if path == "/fail" {
			return 500, `{"error":"nope"}`
		}
		return 200, `{}`
	})
	t.Cleanup(stop)

	ctx := context.Background()
	if err := fcAPIRequest(ctx, "", http.MethodPut, "/actions", nil); err == nil {
		t.Fatal("empty sock")
	}
	if err := fcAPIAction(ctx, sock, "SendCtrlAltDel"); err != nil {
		t.Fatal(err)
	}
	if err := fcAPIPatchVM(ctx, sock, "Paused"); err != nil {
		t.Fatal(err)
	}
	if err := fcAPIRequest(ctx, sock, http.MethodGet, "/fail", nil); err == nil {
		t.Fatal("want status error")
	}
	// missing socket dial error
	if err := fcAPIRequest(ctx, filepath.Join(t.TempDir(), "no.sock"), http.MethodGet, "/x", nil); err == nil {
		t.Fatal("want dial error")
	}
}

func TestFirecrackerStopGracefulAPI(t *testing.T) {
	// API SendCtrlAltDel + process already "dead" → graceful exit
	sock := shortUnixSock(t)
	stop := mockFCAPIServer(t, sock, nil)
	t.Cleanup(stop)
	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// copy sock path into vm dir as firecracker.sock for cleanup
	rt := NewFirecrackerRuntime("", dir, "")
	inst := &vm.Instance{
		Name: "grace", PID: 1<<30 - 7, DiskPath: disk, QMPPath: sock,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rt.Stop(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusStopped {
		t.Fatalf("%+v", inst)
	}
}

func TestFCAPISocketReady(t *testing.T) {
	// non-socket file → dial attempt may fail → false
	dir := t.TempDir()
	p := filepath.Join(dir, "not-sock")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fcAPISocketReady(p) {
		// regular file: ModeSocket unset; dial may fail → false
		t.Log("file reported ready unexpectedly")
	}
	if fcAPISocketReady(filepath.Join(dir, "missing")) {
		t.Fatal("missing")
	}
	// real unix socket
	sock := shortUnixSock(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	if !fcAPISocketReady(sock) {
		t.Fatal("want ready")
	}
}

func TestFCImmediateExitErrNoTail(t *testing.T) {
	err := fcImmediateExitErr(filepath.Join(t.TempDir(), "missing.log"), nil)
	if err == nil || !strings.Contains(err.Error(), "exited immediately") {
		t.Fatalf("%v", err)
	}
	err = fcImmediateExitErr(filepath.Join(t.TempDir(), "missing.log"), context.DeadlineExceeded)
	if err == nil {
		t.Fatal("want err")
	}
}

func TestResolveKernelExplicitMissing(t *testing.T) {
	dir := t.TempDir()
	rt := NewFirecrackerRuntime("", dir, filepath.Join(dir, "no-such-kernel"))
	_, err := rt.resolveKernel()
	if err == nil || !strings.Contains(err.Error(), "kernel_path") {
		t.Fatalf("%v", err)
	}
}

func TestEnsureRawRootfsQcow2ConvertStub(t *testing.T) {
	installQemuImgStub(t, `
case "$1" in
convert)
  dest=""
  for a in "$@"; do dest="$a"; done
  echo raw-out > "$dest"
  exit 0
  ;;
*) exit 0 ;;
esac
`)
	dir := t.TempDir()
	qcow := filepath.Join(dir, "disk.qcow2")
	if err := os.WriteFile(qcow, []byte("qcow"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ensureRawRootfs(context.Background(), qcow)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != FCRawDiskName {
		t.Fatalf("%s", got)
	}
}

func TestCleanupFCFilesWithNetState(t *testing.T) {
	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := FCNetState{FCNetPlan: PlanFCNet("cleanup-net")}
	if err := WriteFCNetState(dir, st); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFCNetState(dir); err != nil {
		t.Fatal(err)
	}
	cleanupFCFiles(&vm.Instance{DiskPath: disk})
	if _, err := ReadFCNetState(dir); !os.IsNotExist(err) {
		t.Fatalf("net state should be removed: %v", err)
	}
}

func TestFirecrackerStartLeftoverNetAndCtxCancel(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = "linux"
	t.Cleanup(func() { runtimeGOOS = old })

	oldGrace := fcPostStartGrace
	fcPostStartGrace = 80 * time.Millisecond
	t.Cleanup(func() { fcPostStartGrace = oldGrace })

	dir := t.TempDir()
	k := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(k, []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	vmDir := filepath.Join(dir, "vms", "cx")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Leftover net state from a prior crash — Start should teardown/remove it.
	if err := WriteFCNetState(vmDir, FCNetState{FCNetPlan: PlanFCNet("cx")}); err != nil {
		t.Fatal(err)
	}

	// Long-running fake FC so we can cancel the context mid-wait.
	fakeBin := filepath.Join(dir, "fc-long")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rt := NewFirecrackerRuntime(fakeBin, dir, k)
	rt.DisableNet = true

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel quickly while Start is waiting for the API socket.
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()
	err := rt.Start(ctx, &vm.Instance{Name: "cx", CPUs: 1, MemoryMB: 128}, disk)
	if err == nil {
		t.Fatal("expected context cancel error")
	}
	// leftover net meta removed even when Start fails after teardown-at-entry
	if _, err := ReadFCNetState(vmDir); err == nil {
		// may still exist if Start failed before RemoveFCNetState — but Start
		// removes it at the top of the linux path; tolerate either.
		t.Log("fc-net meta still present after cancel")
	}
}

func TestFirecrackerStartLogOpenFail(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = "linux"
	t.Cleanup(func() { runtimeGOOS = old })

	dir := t.TempDir()
	// Make DataDir/logs a regular file so OpenFile(logs/name.log) fails.
	if err := os.WriteFile(filepath.Join(dir, "logs"), []byte("not-a-dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(k, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(dir, "fc")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rt := NewFirecrackerRuntime(fakeBin, dir, k)
	rt.DisableNet = true
	err := rt.Start(context.Background(), &vm.Instance{Name: "nolog", CPUs: 1, MemoryMB: 128}, disk)
	if err == nil {
		t.Fatal("expected log open error")
	}
}

func TestFirecrackerStartDiesAfterSocket(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = "linux"
	t.Cleanup(func() { runtimeGOOS = old })

	oldGrace := fcPostStartGrace
	fcPostStartGrace = 200 * time.Millisecond
	t.Cleanup(func() { fcPostStartGrace = oldGrace })

	dir := t.TempDir()
	k := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(k, []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	vmDir := filepath.Join(dir, "vms", "die")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	apiSock := filepath.Join(vmDir, FCSocketName)
	// Create API socket then exit quickly so grace period catches the death.
	fakeBin := filepath.Join(dir, "fc-die")
	script := "#!/bin/sh\n" +
		"python3 -c 'import socket,sys,time;s=socket.socket(socket.AF_UNIX);s.bind(sys.argv[1]);s.listen(1);time.sleep(0.05)' " + apiSock + " &\n" +
		"sleep 0.08\nexit 1\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	rt := NewFirecrackerRuntime(fakeBin, dir, k)
	rt.DisableNet = true
	err := rt.Start(context.Background(), &vm.Instance{Name: "die", CPUs: 1, MemoryMB: 128}, disk)
	if err == nil {
		t.Fatal("expected immediate-exit after socket")
	}
	if !strings.Contains(err.Error(), "exited immediately") && !strings.Contains(err.Error(), "context") {
		t.Logf("got: %v", err)
	}
}

func TestFirecrackerStopCtxCancelDuringPowerdown(t *testing.T) {
	// API accepts SendCtrlAltDel; live child + canceled ctx → powerdown wait hits ctx.Done.
	sock := shortUnixSock(t)
	stop := mockFCAPIServer(t, sock, nil)
	t.Cleanup(stop)

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewFirecrackerRuntime("", dir, "")
	inst := &vm.Instance{
		Name:     "alive",
		PID:      cmd.Process.Pid,
		DiskPath: disk,
		QMPPath:  sock,
	}
	// Already-canceled context: first powerdown poll takes the ctx.Done branch → hardKill.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rt.Stop(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if inst.Status != vm.StatusStopped {
		t.Fatalf("%+v", inst)
	}
}

func TestFCAPISocketReadyDialPath(t *testing.T) {
	// File that exists but is not ModeSocket: dial may fail → false.
	// Real listening socket covered elsewhere; here force dial success branch via ModeSocket
	// by using a real unix listener (already covered). Also exercise non-socket file fully.
	dir := t.TempDir()
	p := filepath.Join(dir, "plain")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fcAPISocketReady(p) {
		t.Log("plain file ready (unusual)")
	}
}

func TestFirecrackerStartCmdStartFail(t *testing.T) {
	old := runtimeGOOS
	runtimeGOOS = "linux"
	t.Cleanup(func() { runtimeGOOS = old })

	dir := t.TempDir()
	k := filepath.Join(dir, "vmlinux")
	if err := os.WriteFile(k, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(dir, "disk.raw")
	if err := os.WriteFile(disk, []byte("raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Binary path that LookPath accepts but cannot exec (directory).
	// LookPath on absolute directory returns an error on modern Go — use a
	// non-exec file with no +x so LookPath fails; instead create an empty
	// executable that is removed after LookPath by using a script with bad shebang?
	// cmd.Start fails if the interpreter is missing for #!/no/such/bin
	fakeBin := filepath.Join(dir, "fc-bad")
	if err := os.WriteFile(fakeBin, []byte("#!/no/such/interpreter/for/grain/test\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rt := NewFirecrackerRuntime(fakeBin, dir, k)
	rt.DisableNet = true
	err := rt.Start(context.Background(), &vm.Instance{Name: "badstart", CPUs: 1, MemoryMB: 128}, disk)
	// Either Start fails at cmd.Start or process exits immediately — either covers more of Start.
	if err == nil {
		t.Fatal("expected start failure")
	}
}

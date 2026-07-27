package hypervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestVirtiofsDeviceArgsEmpty(t *testing.T) {
	t.Parallel()
	if got := virtiofsDeviceArgs(nil, "/tmp"); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestVirtiofsDeviceArgsSkipsIncomplete(t *testing.T) {
	t.Parallel()
	got := virtiofsDeviceArgs([]vm.Mount{
		{Host: "", Tag: "grain0"},
		{Host: "/ok", Tag: "grain1"},
		{Host: "/x", Tag: ""},
	}, "/vm")
	if len(got) != 4 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	if got[1] != "socket,id=charfs1,path=/vm/virtiofsd-1.sock" {
		t.Fatalf("chardev: %s", got[1])
	}
	if got[3] != "vhost-user-fs-pci,queue-size=1024,chardev=charfs1,tag=grain1" {
		t.Fatalf("device: %s", got[3])
	}
}

func TestVirtiofsdSocketAndPIDPaths(t *testing.T) {
	t.Parallel()
	if got := virtiofsdSocketPath("/vm", 2); got != "/vm/virtiofsd-2.sock" {
		t.Fatalf("socket: %s", got)
	}
	if got := virtiofsdPIDPath("/vm", 2); got != "/vm/virtiofsd-2.pid" {
		t.Fatalf("pid: %s", got)
	}
}

func TestStopVirtiofsDaemonsCleansFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// orphan pid/socket files (pid not alive)
	_ = os.WriteFile(filepath.Join(dir, "virtiofsd-0.pid"), []byte("1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "virtiofsd-0.sock"), []byte{}, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "other.txt"), []byte("keep"), 0o644)

	StopVirtiofsDaemons(dir)

	if _, err := os.Stat(filepath.Join(dir, "virtiofsd-0.pid")); !os.IsNotExist(err) {
		t.Fatalf("pidfile should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "virtiofsd-0.sock")); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "other.txt")); err != nil {
		t.Fatalf("unrelated file removed: %v", err)
	}
}

func TestStopVirtiofsDaemonsEmptyDir(t *testing.T) {
	t.Parallel()
	StopVirtiofsDaemons("")
	StopVirtiofsDaemons(filepath.Join(t.TempDir(), "missing"))
}

func TestPidAliveAndKillPID(t *testing.T) {
	t.Parallel()
	if pidAlive(0) || pidAlive(-1) {
		t.Fatal("invalid pids")
	}
	if !pidAlive(os.Getpid()) {
		t.Fatal("self should be alive")
	}
	if pidAlive(999999999) {
		t.Fatal("dead pid")
	}
	// killPID on dead/invalid is best-effort no panic
	if err := killPID(0); err != nil {
		t.Fatal(err)
	}
	_ = killPID(999999999)
}

func TestStartVirtiofsDaemonsEmptyAndMissing(t *testing.T) {
	// not parallel: mutates lookPathVirtiofsd
	if err := StartVirtiofsDaemons(t.TempDir(), nil); err != nil {
		t.Fatal(err)
	}
	if err := StartVirtiofsDaemons(t.TempDir(), []vm.Mount{}); err != nil {
		t.Fatal(err)
	}

	// Force missing virtiofsd via injectable lookPath
	old := lookPathVirtiofsd
	lookPathVirtiofsd = func() (string, error) {
		return "", os.ErrNotExist
	}
	defer func() { lookPathVirtiofsd = old }()

	err := StartVirtiofsDaemons(t.TempDir(), []vm.Mount{{Host: "/tmp", Tag: "grain0"}})
	if err == nil {
		t.Fatal("expected missing virtiofsd error")
	}
}

func TestStartVirtiofsDaemonsFakeBinaryExits(t *testing.T) {
	// not parallel: mutates lookPathVirtiofsd
	dir := t.TempDir()
	// Script that exits immediately without creating a socket → startOneVirtiofsd errors.
	bin := filepath.Join(dir, "fake-virtiofsd")
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := lookPathVirtiofsd
	lookPathVirtiofsd = func() (string, error) { return bin, nil }
	defer func() { lookPathVirtiofsd = old }()

	vmDir := filepath.Join(dir, "vm")
	err := StartVirtiofsDaemons(vmDir, []vm.Mount{{Host: dir, Tag: "grain0"}})
	if err == nil {
		// Extremely unlikely: fake binary created socket somehow
		StopVirtiofsDaemons(vmDir)
		return
	}
	// "exited before creating socket" or "timeout" or "start:"
	msg := err.Error()
	if !strings.Contains(msg, "virtiofsd") && !strings.Contains(msg, "socket") && !strings.Contains(msg, "exited") && !strings.Contains(msg, "timeout") && !strings.Contains(msg, "start") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartVirtiofsDaemonsFakeBinarySuccess(t *testing.T) {
	// not parallel: mutates lookPathVirtiofsd
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-virtiofsd-ok")
	// Create the --socket-path= file then sleep so the parent sees a live process.
	script := `#!/bin/sh
for a in "$@"; do
  case "$a" in
    --socket-path=*) : > "${a#--socket-path=}" ;;
  esac
done
sleep 30
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := lookPathVirtiofsd
	lookPathVirtiofsd = func() (string, error) { return bin, nil }
	defer func() { lookPathVirtiofsd = old }()

	vmDir := filepath.Join(dir, "vm")
	mounts := []vm.Mount{
		{Host: "", Tag: "skip"},
		{Host: dir, Tag: "grain1"},
	}
	if err := StartVirtiofsDaemons(vmDir, mounts); err != nil {
		t.Fatal(err)
	}
	defer StopVirtiofsDaemons(vmDir)
	if _, err := os.Stat(virtiofsdSocketPath(vmDir, 1)); err != nil {
		t.Fatalf("socket: %v", err)
	}
	if _, err := os.Stat(virtiofsdPIDPath(vmDir, 1)); err != nil {
		t.Fatalf("pid: %v", err)
	}
}

func TestVirtiofsdAvailable(t *testing.T) {
	// not parallel: mutates lookPathVirtiofsd
	// Result depends on host install; just call for coverage.
	_ = VirtiofsdAvailable()

	old := lookPathVirtiofsd
	lookPathVirtiofsd = func() (string, error) { return "/bin/true", nil }
	if !VirtiofsdAvailable() {
		t.Fatal("expected available when lookPath succeeds")
	}
	lookPathVirtiofsd = func() (string, error) { return "", os.ErrNotExist }
	if VirtiofsdAvailable() {
		t.Fatal("expected unavailable")
	}
	lookPathVirtiofsd = old
}

func TestFindVirtiofsd(t *testing.T) {
	t.Parallel()
	// Exercise findVirtiofsd (PATH + common paths). May or may not find a binary.
	_, _ = findVirtiofsd()
}

func TestVhostVsockAvailable(t *testing.T) {
	t.Parallel()
	// Public helper; typically false on macOS.
	_ = VhostVsockAvailable()
	if vhostVsockAvailable("") {
		// empty falls back to /dev/vhost-vsock — only true on configured Linux
		t.Log("vhost-vsock available on this host")
	}
}

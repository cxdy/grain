package hypervisor

import (
	"os"
	"path/filepath"
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

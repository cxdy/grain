package hypervisor

import (
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestVirtio9pArgsEmpty(t *testing.T) {
	t.Parallel()
	if got := virtio9pArgs(nil); got != nil {
		t.Fatalf("got %v", got)
	}
	if got := virtio9pArgs([]vm.Mount{}); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestVirtio9pArgsSingle(t *testing.T) {
	t.Parallel()
	got := virtio9pArgs([]vm.Mount{
		{Host: "/Users/me/src", Guest: "/mnt/src", Tag: "grain0"},
	})
	if len(got) != 4 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	if got[0] != "-fsdev" || got[2] != "-device" {
		t.Fatalf("flags: %v", got)
	}
	if got[1] != "local,id=fs0,path=/Users/me/src,security_model=mapped-xattr" {
		t.Fatalf("fsdev: %s", got[1])
	}
	if got[3] != "virtio-9p-pci,fsdev=fs0,mount_tag=grain0" {
		t.Fatalf("device: %s", got[3])
	}
	// must use mapped-xattr (not passthrough) for macOS
	if strings.Contains(strings.Join(got, " "), "passthrough") {
		t.Fatal("must not use passthrough security_model")
	}
}

func TestVirtio9pArgsMultiple(t *testing.T) {
	t.Parallel()
	got := virtio9pArgs([]vm.Mount{
		{Host: "/a", Guest: "/mnt/a", Tag: "grain0"},
		{Host: "/b", Guest: "/mnt/b", Tag: "grain1"},
	})
	if len(got) != 8 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	joined := strings.Join(got, " ")
	for _, frag := range []string{
		"id=fs0,path=/a,security_model=mapped-xattr",
		"mount_tag=grain0",
		"id=fs1,path=/b,security_model=mapped-xattr",
		"mount_tag=grain1",
	} {
		if !strings.Contains(joined, frag) {
			t.Fatalf("missing %q in %s", frag, joined)
		}
	}
}

func TestVirtio9pArgsSkipsIncomplete(t *testing.T) {
	t.Parallel()
	got := virtio9pArgs([]vm.Mount{
		{Host: "", Guest: "/mnt/x", Tag: "grain0"},
		{Host: "/ok", Guest: "/mnt/ok", Tag: "grain1"},
		{Host: "/no-tag", Guest: "/mnt/y", Tag: ""},
	})
	// only index 1 (fs1) should appear; empty host/tag skipped but index preserved
	if len(got) != 4 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	if !strings.Contains(got[1], "id=fs1,path=/ok") {
		t.Fatalf("expected fs1 for second entry: %v", got)
	}
}

func TestResolveMountDriver(t *testing.T) {
	// not parallel: mutates lookPathVirtiofsd
	orig := lookPathVirtiofsd
	t.Cleanup(func() { lookPathVirtiofsd = orig })

	if got := ResolveMountDriver("", nil); got != MountDriver9p {
		t.Fatalf("empty → %s", got)
	}
	if got := ResolveMountDriver("9p", nil); got != MountDriver9p {
		t.Fatalf("9p → %s", got)
	}
	if got := ResolveMountDriver("unknown", nil); got != MountDriver9p {
		t.Fatalf("unknown → %s", got)
	}

	// virtiofs unavailable → 9p (all platforms when binary missing)
	lookPathVirtiofsd = func() (string, error) {
		return "", errVirtiofsdMissing
	}
	if got := ResolveMountDriver("virtiofs", nil); got != MountDriver9p {
		t.Fatalf("virtiofs without binary → %s", got)
	}

	// virtiofs available
	lookPathVirtiofsd = func() (string, error) {
		return "/usr/libexec/virtiofsd", nil
	}
	got := ResolveMountDriver("virtiofs", nil)
	if runtime.GOOS == "darwin" {
		if got != MountDriver9p {
			t.Fatalf("darwin virtiofs must fall back to 9p, got %s", got)
		}
	} else {
		if got != MountDriverVirtioFS {
			t.Fatalf("linux+virtiofsd → %s", got)
		}
	}
}

// errVirtiofsdMissing is a sentinel for tests (avoids importing errors in setup).
var errVirtiofsdMissing = errString("virtiofsd not found")

type errString string

func (e errString) Error() string { return string(e) }

func TestFsdevArgsUses9p(t *testing.T) {
	t.Parallel()
	got := fsdevArgs([]vm.Mount{{Host: "/a", Guest: "/mnt/a", Tag: "grain0"}}, MountDriver9p, "/tmp/vm")
	if len(got) != 4 || !strings.Contains(got[3], "virtio-9p-pci") {
		t.Fatalf("%v", got)
	}
}

func TestFsdevArgsVirtiofs(t *testing.T) {
	t.Parallel()
	vmDir := "/var/lib/grain/vms/sbox-1"
	got := fsdevArgs([]vm.Mount{
		{Host: "/home/src", Guest: "/work", Tag: "grain0"},
		{Host: "/data", Guest: "/data", Tag: "grain1"},
	}, MountDriverVirtioFS, vmDir)
	if len(got) != 8 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	joined := strings.Join(got, " ")
	for _, frag := range []string{
		"-chardev",
		"socket,id=charfs0,path=" + vmDir + "/virtiofsd-0.sock",
		"vhost-user-fs-pci,queue-size=1024,chardev=charfs0,tag=grain0",
		"socket,id=charfs1,path=" + vmDir + "/virtiofsd-1.sock",
		"vhost-user-fs-pci,queue-size=1024,chardev=charfs1,tag=grain1",
	} {
		if !strings.Contains(joined, frag) {
			t.Fatalf("missing %q in %s", frag, joined)
		}
	}
	if strings.Contains(joined, "virtio-9p") {
		t.Fatalf("virtiofs args must not include 9p: %s", joined)
	}
}

func TestVirtiofsMemoryBackendArgs(t *testing.T) {
	t.Parallel()
	got := virtiofsMemoryBackendArgs(2048)
	if len(got) != 4 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	if got[0] != "-object" || got[2] != "-numa" {
		t.Fatalf("flags: %v", got)
	}
	if got[1] != "memory-backend-file,id=mem,size=2048M,mem-path=/dev/shm,share=on" {
		t.Fatalf("object: %s", got[1])
	}
	if got[3] != "node,memdev=mem" {
		t.Fatalf("numa: %s", got[3])
	}
}

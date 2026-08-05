package hypervisor

import (
	"io"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestVirtio9pArgsEmpty(t *testing.T) {
	t.Parallel()
	got, err := virtio9pArgs(nil)
	if err != nil || got != nil {
		t.Fatalf("got %v err %v", got, err)
	}
	got, err = virtio9pArgs([]vm.Mount{})
	if err != nil || got != nil {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestVirtio9pArgsSingle(t *testing.T) {
	t.Parallel()
	got, err := virtio9pArgs([]vm.Mount{
		{Host: "/Users/me/src", Guest: "/mnt/src", Tag: "grain0"},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	got, err := virtio9pArgs([]vm.Mount{
		{Host: "/a", Guest: "/mnt/a", Tag: "grain0"},
		{Host: "/b", Guest: "/mnt/b", Tag: "grain1"},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	got, err := virtio9pArgs([]vm.Mount{
		{Host: "", Guest: "/mnt/x", Tag: "grain0"},
		{Host: "/ok", Guest: "/mnt/ok", Tag: "grain1"},
		{Host: "/no-tag", Guest: "/mnt/y", Tag: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	// only index 1 (fs1) should appear; empty host/tag skipped but index preserved
	if len(got) != 4 {
		t.Fatalf("len %d: %v", len(got), got)
	}
	if !strings.Contains(got[1], "id=fs1,path=/ok") {
		t.Fatalf("expected fs1 for second entry: %v", got)
	}
}

func TestVirtio9pArgsRejectsUnsafePathAndTag(t *testing.T) {
	t.Parallel()
	// path with comma injects extra QEMU option fields
	_, err := virtio9pArgs([]vm.Mount{
		{Host: "/tmp/evil,id=x", Tag: "grain0"},
	})
	if err == nil || !strings.Contains(err.Error(), "host path contains forbidden character") {
		t.Fatalf("comma path: %v", err)
	}
	// tag with comma / space
	_, err = virtio9pArgs([]vm.Mount{
		{Host: "/Users/me/src", Tag: "grain,0"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid tag") {
		t.Fatalf("comma tag: %v", err)
	}
	_, err = virtio9pArgs([]vm.Mount{
		{Host: "/Users/me/src", Tag: "grain 0"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid tag") {
		t.Fatalf("space tag: %v", err)
	}
	// normal path + grain0 accepted
	got, err := virtio9pArgs([]vm.Mount{
		{Host: "/Users/me/src", Guest: "/mnt/src", Tag: "grain0"},
	})
	if err != nil || len(got) != 4 {
		t.Fatalf("accept normal: got %v err %v", got, err)
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
	// With logger (covers warn branches)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if got := ResolveMountDriver("virtiofs", log); got != MountDriver9p {
		t.Fatalf("virtiofs without binary + log → %s", got)
	}

	// virtiofs available
	lookPathVirtiofsd = func() (string, error) {
		return "/usr/libexec/virtiofsd", nil
	}
	got := ResolveMountDriver("virtiofs", log)
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
	got, err := fsdevArgs([]vm.Mount{{Host: "/a", Guest: "/mnt/a", Tag: "grain0"}}, MountDriver9p, "/tmp/vm")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || !strings.Contains(got[3], "virtio-9p-pci") {
		t.Fatalf("%v", got)
	}
}

func TestFsdevArgsVirtiofs(t *testing.T) {
	t.Parallel()
	vmDir := "/var/lib/grain/vms/sbox-1"
	got, err := fsdevArgs([]vm.Mount{
		{Host: "/home/src", Guest: "/work", Tag: "grain0"},
		{Host: "/data", Guest: "/data", Tag: "grain1"},
	}, MountDriverVirtioFS, vmDir)
	if err != nil {
		t.Fatal(err)
	}
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

func TestValidateMount(t *testing.T) {
	t.Parallel()
	// accept normal /Users/me/src + grain0
	if err := ValidateMount(vm.Mount{Host: "/Users/me/src", Tag: "grain0"}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	// reject path with comma
	err := ValidateMount(vm.Mount{Host: "/tmp/a,b", Tag: "grain0"})
	if err == nil || !strings.Contains(err.Error(), "host path contains forbidden character") {
		t.Fatalf("comma path: %v", err)
	}
	// reject path with equals
	err = ValidateMount(vm.Mount{Host: "/tmp/a=b", Tag: "grain0"})
	if err == nil || !strings.Contains(err.Error(), "host path contains forbidden character") {
		t.Fatalf("equals path: %v", err)
	}
	// reject path with newline
	err = ValidateMount(vm.Mount{Host: "/tmp/a\nb", Tag: "grain0"})
	if err == nil || !strings.Contains(err.Error(), "host path contains forbidden character") {
		t.Fatalf("newline path: %v", err)
	}
	// reject path with CR / NUL
	err = ValidateMount(vm.Mount{Host: "/tmp/a\rb", Tag: "grain0"})
	if err == nil || !strings.Contains(err.Error(), "host path contains forbidden character") {
		t.Fatalf("CR path: %v", err)
	}
	err = ValidateMount(vm.Mount{Host: "/tmp/a\x00b", Tag: "grain0"})
	if err == nil || !strings.Contains(err.Error(), "host path contains forbidden character") {
		t.Fatalf("NUL path: %v", err)
	}
	// reject tag with comma or space
	err = ValidateMount(vm.Mount{Host: "/Users/me/src", Tag: "bad,tag"})
	if err == nil || !strings.Contains(err.Error(), "invalid tag") {
		t.Fatalf("comma tag: %v", err)
	}
	err = ValidateMount(vm.Mount{Host: "/Users/me/src", Tag: "bad tag"})
	if err == nil || !strings.Contains(err.Error(), "invalid tag") {
		t.Fatalf("space tag: %v", err)
	}
	// empty tag / empty host
	if err := ValidateMount(vm.Mount{Host: "/x", Tag: ""}); err == nil || !strings.Contains(err.Error(), "invalid tag") {
		t.Fatalf("empty tag: %v", err)
	}
	if err := ValidateMount(vm.Mount{Host: "", Tag: "grain0"}); err == nil {
		t.Fatal("empty host")
	}
	// tag length bound
	if err := ValidateMount(vm.Mount{Host: "/x", Tag: strings.Repeat("a", 65)}); err == nil {
		t.Fatal("tag too long")
	}
	if err := ValidateMount(vm.Mount{Host: "/x", Tag: strings.Repeat("a", 64)}); err != nil {
		t.Fatalf("64-char tag: %v", err)
	}
	// ValidateMounts indexes errors
	err = ValidateMounts([]vm.Mount{
		{Host: "/ok", Tag: "grain0"},
		{Host: "/bad,path", Tag: "grain1"},
	})
	if err == nil || !strings.Contains(err.Error(), "mount[1]") {
		t.Fatalf("ValidateMounts: %v", err)
	}
	if err := ValidateMounts(nil); err != nil {
		t.Fatal(err)
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

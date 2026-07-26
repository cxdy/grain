package hypervisor

import (
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

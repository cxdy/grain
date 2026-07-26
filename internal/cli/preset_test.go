package cli

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestApplyPresetDockerMergesUserdata(t *testing.T) {
	t.Parallel()
	ud, cpus, mem, fwds, err := applyPreset("docker", "", 0, 0, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud, "docker") {
		t.Fatalf("userdata:\n%s", ud)
	}
	if cpus != 0 || mem != 0 {
		t.Fatalf("docker should not force resources, got %d %d", cpus, mem)
	}
	if len(fwds) != 0 {
		t.Fatalf("fwds %v", fwds)
	}

	// user extra merged in
	ud, _, _, _, err = applyPreset("docker", "echo hello-from-user", 0, 0, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud, "docker") || !strings.Contains(ud, "hello-from-user") {
		t.Fatalf("merged userdata:\n%s", ud)
	}
}

func TestApplyPresetK3sResourcesAndPort(t *testing.T) {
	t.Parallel()
	ud, cpus, mem, fwds, err := applyPreset("k3s", "", 0, 0, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud, "k3s") {
		t.Fatalf("userdata:\n%s", ud)
	}
	if cpus != 2 || mem != 4096 {
		t.Fatalf("k3s defaults cpus=%d mem=%d", cpus, mem)
	}
	if !hasGuestPort(fwds, 6443) {
		t.Fatalf("expected auto 6443 forward, got %+v", fwds)
	}

	// explicit resources and existing 6443 not overridden
	existing := []vm.PortForward{{HostPort: 6443, GuestPort: 6443, Proto: "tcp"}}
	_, cpus, mem, fwds, err = applyPreset("k3s", "", 4, 8192, true, true, existing)
	if err != nil {
		t.Fatal(err)
	}
	if cpus != 4 || mem != 8192 {
		t.Fatalf("want explicit resources, got %d %d", cpus, mem)
	}
	if len(fwds) != 1 || fwds[0].HostPort != 6443 {
		t.Fatalf("should keep existing 6443: %+v", fwds)
	}

	// profile-resolved resources (set but not via flag tracking: cpusSet true when profile filled)
	_, cpus, mem, _, err = applyPreset("k3s", "", 4, 0, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cpus != 4 {
		t.Fatalf("cpus %d", cpus)
	}
	if mem != 4096 {
		t.Fatalf("mem should fill k3s default when unset: %d", mem)
	}
}

func TestApplyPresetUnknown(t *testing.T) {
	t.Parallel()
	_, _, _, _, err := applyPreset("nope", "", 0, 0, false, false, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyPresetEmptyNoop(t *testing.T) {
	t.Parallel()
	ud, cpus, mem, fwds, err := applyPreset("", "keep-me", 1, 2, true, true, []vm.PortForward{{GuestPort: 80}})
	if err != nil {
		t.Fatal(err)
	}
	if ud != "keep-me" || cpus != 1 || mem != 2 || len(fwds) != 1 {
		t.Fatalf("noop failed: ud=%q c=%d m=%d fwds=%+v", ud, cpus, mem, fwds)
	}
}

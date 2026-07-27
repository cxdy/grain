package hypervisor

import "testing"

func TestResolveGuestArch(t *testing.T) {
	t.Parallel()
	if got := resolveGuestArch(""); got != hostArch() {
		t.Fatalf("empty: %s", got)
	}
	if got := resolveGuestArch("x86_64"); got != "amd64" {
		t.Fatalf("x86_64: %s", got)
	}
	if got := resolveGuestArch("aarch64"); got != "arm64" {
		t.Fatalf("aarch64: %s", got)
	}
}

func TestMachineTypeCross(t *testing.T) {
	t.Parallel()
	if got := machineTypeFor("amd64", true); got != "q35,accel=tcg" {
		t.Fatalf("cross amd64: %s", got)
	}
	if got := cpuTypeFor("amd64", true); got != "qemu64" {
		t.Fatalf("cross cpu: %s", got)
	}
}

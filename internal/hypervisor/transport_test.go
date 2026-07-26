package hypervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAgentTransport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		req   string
		vhost bool
		want  string
	}{
		{"auto_no_vhost", "auto", false, AgentTransportTCP},
		{"auto_vhost", "auto", true, AgentTransportVsock},
		{"empty_no_vhost", "", false, AgentTransportTCP},
		{"empty_vhost", "", true, AgentTransportVsock},
		{"tcp_forces_tcp", "tcp", true, AgentTransportTCP},
		{"tcp_no_vhost", "tcp", false, AgentTransportTCP},
		{"vsock_forces_vsock", "vsock", false, AgentTransportVsock},
		{"vsock_with_vhost", "vsock", true, AgentTransportVsock},
		{"TCP_upper", "TCP", true, AgentTransportTCP},
		{"VSOCK_upper", "VSOCK", false, AgentTransportVsock},
		{"unknown_as_auto", "weird", true, AgentTransportVsock},
		{"unknown_as_auto_no", "weird", false, AgentTransportTCP},
		{"spaces", "  auto  ", true, AgentTransportVsock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveAgentTransport(tc.req, tc.vhost)
			if got != tc.want {
				t.Fatalf("ResolveAgentTransport(%q, %v) = %q, want %q", tc.req, tc.vhost, got, tc.want)
			}
		})
	}
}

func TestAllocateGuestCID(t *testing.T) {
	t.Parallel()
	a := AllocateGuestCID("sbox-1")
	b := AllocateGuestCID("sbox-1")
	if a != b {
		t.Fatalf("not stable: %d vs %d", a, b)
	}
	if a < MinGuestCID {
		t.Fatalf("cid %d < MinGuestCID %d", a, MinGuestCID)
	}
	c := AllocateGuestCID("other-vm")
	if c < MinGuestCID {
		t.Fatalf("cid %d < MinGuestCID", c)
	}
	if a == c {
		t.Logf("note: hash collision for sbox-1 and other-vm → %d", a)
	}
}

func TestVhostVsockAvailableInjected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "nope")
	if vhostVsockAvailable(missing) {
		t.Fatal("expected missing path to be unavailable")
	}
	present := filepath.Join(dir, "vhost-vsock")
	if err := os.WriteFile(present, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !vhostVsockAvailable(present) {
		t.Fatal("expected present path to be available")
	}
}

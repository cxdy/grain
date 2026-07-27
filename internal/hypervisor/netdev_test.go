package hypervisor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestBuildUserNetdevSSHAndAgent(t *testing.T) {
	t.Parallel()
	got := buildUserNetdev(2222, 17475, nil)
	want := "user,id=net0,hostfwd=tcp:127.0.0.1:2222-:22,hostfwd=tcp:127.0.0.1:17475-:7475"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildUserNetdevSSHPlusExtra(t *testing.T) {
	t.Parallel()
	fwds := []vm.PortForward{
		{HostPort: 8080, GuestPort: 80},
		{HostPort: 8443, GuestPort: 443, Proto: "tcp"},
		{HostPort: 5353, GuestPort: 53, Proto: "udp"},
	}
	got := buildUserNetdev(2200, 17475, fwds)
	if !strings.HasPrefix(got, "user,id=net0,hostfwd=tcp:127.0.0.1:2200-:22,hostfwd=tcp:127.0.0.1:17475-:7475") {
		t.Fatalf("missing ssh/agent hostfwd: %s", got)
	}
	for _, frag := range []string{
		"hostfwd=tcp:127.0.0.1:8080-:80",
		"hostfwd=tcp:127.0.0.1:8443-:443",
		"hostfwd=udp:127.0.0.1:5353-:53",
	} {
		if !strings.Contains(got, frag) {
			t.Fatalf("missing %q in %s", frag, got)
		}
	}
}

func TestBuildUserNetdevSkipsZeroHost(t *testing.T) {
	t.Parallel()
	// unallocated HostPort 0 should be skipped (caller should allocate first)
	got := buildUserNetdev(22, 17475, []vm.PortForward{{HostPort: 0, GuestPort: 80}})
	want := "user,id=net0,hostfwd=tcp:127.0.0.1:22-:22,hostfwd=tcp:127.0.0.1:17475-:7475"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildUserNetdevAgentPortAlwaysPresent(t *testing.T) {
	t.Parallel()
	got := buildUserNetdev(2222, 19000, nil)
	agentFwd := fmt.Sprintf("hostfwd=tcp:127.0.0.1:19000-:%d", GuestAgentPort)
	if !strings.Contains(got, agentFwd) {
		t.Fatalf("missing agent hostfwd %q in %s", agentFwd, got)
	}
	if GuestAgentPort != 7475 {
		t.Fatalf("GuestAgentPort = %d, want 7475", GuestAgentPort)
	}
}

func TestValidateForwardsPrivileged(t *testing.T) {
	t.Parallel()
	err := ValidateForwards([]vm.PortForward{{HostPort: 80, GuestPort: 80}})
	if err == nil {
		t.Fatal("expected privileged host port error")
	}
	if !strings.Contains(err.Error(), "privileged") {
		t.Fatalf("err %v", err)
	}
}

func TestValidateForwardsAutoOK(t *testing.T) {
	t.Parallel()
	if err := ValidateForwards([]vm.PortForward{{HostPort: 0, GuestPort: 80}}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateForwards([]vm.PortForward{{HostPort: 8080, GuestPort: 80}}); err != nil {
		t.Fatal(err)
	}
}

func TestAllocateForwardPorts(t *testing.T) {
	t.Parallel()
	fwds := []vm.PortForward{
		{HostPort: 0, GuestPort: 80},
		{HostPort: 9090, GuestPort: 9090},
	}
	if err := AllocateForwardPorts(fwds); err != nil {
		t.Fatal(err)
	}
	if fwds[0].HostPort < 1024 {
		t.Fatalf("auto host port %d should be free high port", fwds[0].HostPort)
	}
	if fwds[1].HostPort != 9090 {
		t.Fatalf("fixed port changed: %d", fwds[1].HostPort)
	}
}

func TestValidateForwardsInvalidGuestAndProto(t *testing.T) {
	t.Parallel()
	if err := ValidateForwards([]vm.PortForward{{GuestPort: 0, HostPort: 8080}}); err == nil {
		t.Fatal("guest 0")
	}
	if err := ValidateForwards([]vm.PortForward{{GuestPort: 70000, HostPort: 8080}}); err == nil {
		t.Fatal("guest too high")
	}
	if err := ValidateForwards([]vm.PortForward{{GuestPort: 80, HostPort: -1}}); err == nil {
		t.Fatal("host -1")
	}
	if err := ValidateForwards([]vm.PortForward{{GuestPort: 80, HostPort: 70000}}); err == nil {
		t.Fatal("host too high")
	}
	if err := ValidateForwards([]vm.PortForward{{GuestPort: 80, HostPort: 8080, Proto: "sctp"}}); err == nil {
		t.Fatal("bad proto")
	}
	if err := ValidateForwards([]vm.PortForward{{GuestPort: 80, HostPort: 8080, Proto: "udp"}}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildUserNetdevUDP(t *testing.T) {
	t.Parallel()
	s := buildUserNetdev(2222, 0, []vm.PortForward{
		{HostPort: 5353, GuestPort: 53, Proto: "udp"},
	})
	if !strings.Contains(s, "hostfwd=udp:127.0.0.1:5353-:53") {
		t.Fatalf("%s", s)
	}
	// agentPort 0 omits agent hostfwd
	if strings.Contains(s, "7475") {
		t.Fatalf("unexpected agent fwd: %s", s)
	}
}

func TestAllocateForwardPortsPrivileged(t *testing.T) {
	t.Parallel()
	err := AllocateForwardPorts([]vm.PortForward{{HostPort: 443, GuestPort: 443}})
	if err == nil {
		t.Fatal("expected error")
	}
}

package hypervisor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestPlanFCNetDeterministic(t *testing.T) {
	t.Parallel()
	a := PlanFCNet("web")
	b := PlanFCNet("web")
	if a != b {
		t.Fatalf("not deterministic: %+v vs %+v", a, b)
	}
	if a.Slot < fcNetSlotMin || a.Slot > fcNetSlotMax {
		t.Fatalf("slot %d out of range", a.Slot)
	}
	if !strings.HasPrefix(a.TapName, "tg") || len(a.TapName) > 15 {
		t.Fatalf("bad tap name %q", a.TapName)
	}
	if !strings.HasPrefix(a.GuestMAC, "02:fc:") {
		t.Fatalf("mac %q", a.GuestMAC)
	}
	wantHost := fmt.Sprintf("10.77.%d.1", a.Slot)
	wantGuest := fmt.Sprintf("10.77.%d.2", a.Slot)
	if a.HostIP != wantHost {
		t.Fatalf("host IP %q want %q", a.HostIP, wantHost)
	}
	if a.GuestIP != wantGuest {
		t.Fatalf("guest IP %q want %q", a.GuestIP, wantGuest)
	}
}

func TestPlanFCNetIPsFormat(t *testing.T) {
	t.Parallel()
	p := PlanFCNet("alpha")
	if !strings.HasPrefix(p.HostIP, "10.77.") || !strings.HasSuffix(p.HostIP, ".1") {
		t.Fatalf("host %q", p.HostIP)
	}
	if !strings.HasPrefix(p.GuestIP, "10.77.") || !strings.HasSuffix(p.GuestIP, ".2") {
		t.Fatalf("guest %q", p.GuestIP)
	}
}

func TestBuildDNATRules(t *testing.T) {
	t.Parallel()
	rules := BuildDNATRules("10.77.1.2", 2200, 17475, []vm.PortForward{
		{HostPort: 8080, GuestPort: 80},
		{HostPort: 5353, GuestPort: 53, Proto: "udp"},
		{HostPort: 0, GuestPort: 99}, // skipped
	})
	if len(rules) != 4 {
		t.Fatalf("got %d rules: %+v", len(rules), rules)
	}
	if rules[0].GuestPort != 22 || rules[0].HostPort != 2200 {
		t.Fatalf("ssh rule %+v", rules[0])
	}
	if rules[1].GuestPort != GuestAgentPort {
		t.Fatalf("agent rule %+v", rules[1])
	}
	if rules[2].Proto != "tcp" || rules[2].GuestPort != 80 {
		t.Fatalf("http rule %+v", rules[2])
	}
	if rules[3].Proto != "udp" {
		t.Fatalf("udp rule %+v", rules[3])
	}
	// Skip SSH/agent when ports zero.
	r2 := BuildDNATRules("10.77.1.2", 0, 0, []vm.PortForward{{HostPort: 9000, GuestPort: 9000}})
	if len(r2) != 1 || r2[0].HostPort != 9000 {
		t.Fatalf("%+v", r2)
	}
}

func TestGuestNetConfigScript(t *testing.T) {
	t.Parallel()
	p := PlanFCNet("s1")
	s := GuestNetConfigScript(p)
	if !strings.Contains(s, p.GuestIP) || !strings.Contains(s, p.HostIP) {
		t.Fatalf("script missing IPs: %s", s)
	}
	if !strings.Contains(s, "ip addr add") {
		t.Fatalf("expected ip addr add: %s", s)
	}
	if !strings.Contains(s, p.IfaceID) {
		t.Fatalf("missing iface: %s", s)
	}
}

func TestPrivilegeErrorHint(t *testing.T) {
	t.Parallel()
	err := PrivilegeErrorHint("create tap", errString("operation not permitted"))
	if err == nil || !strings.Contains(err.Error(), "CAP_NET_ADMIN") {
		t.Fatalf("%v", err)
	}
	err2 := PrivilegeErrorHint("x", errString("device busy"))
	if err2 == nil || strings.Contains(err2.Error(), "CAP_NET_ADMIN") {
		t.Fatalf("unexpected privilege hint: %v", err2)
	}
}

func TestParseHostPortPair(t *testing.T) {
	t.Parallel()
	s, err := ParseHostPortPair(8080)
	if err != nil || s != "127.0.0.1:8080" {
		t.Fatalf("%q %v", s, err)
	}
	if _, err := ParseHostPortPair(0); err == nil {
		t.Fatal("expected error")
	}
}

func TestFCNetSlotRange(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"a", "b", "work", "fc-smoke-1", strings.Repeat("x", 40)} {
		s := FCNetSlotForName(name)
		if s < fcNetSlotMin || s > fcNetSlotMax {
			t.Fatalf("%q → slot %d", name, s)
		}
	}
}

func TestFCTapNameLength(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"x", "very-long-vm-name-that-exceeds-ifnamsize-limits"} {
		n := FCTapName(name)
		if len(n) > 15 {
			t.Fatalf("%q → %q len %d", name, n, len(n))
		}
	}
}

// TestHostToGuestSNATRule documents the create-time publish reply-path fix:
// DNATed loopback clients keep saddr=127.0.0.1 unless SNAT rewrites to HostIP.
func TestHostToGuestSNATRule(t *testing.T) {
	t.Parallel()
	p := PlanFCNet("snat-web")
	rule := HostToGuestSNATRule(p)
	joined := strings.Join(rule, " ")
	if !strings.Contains(joined, p.TapName) {
		t.Fatalf("missing tap %q in %v", p.TapName, rule)
	}
	if !strings.Contains(joined, "127.0.0.1") {
		t.Fatalf("missing loopback match in %v", rule)
	}
	if !strings.Contains(joined, "SNAT") {
		t.Fatalf("missing SNAT target in %v", rule)
	}
	if !strings.Contains(joined, p.HostIP) {
		t.Fatalf("missing HostIP %q as --to-source in %v", p.HostIP, rule)
	}
	// Full iptables shape used by ensureHostToGuestSNAT.
	full := append([]string{"-t", "nat", "-A", "POSTROUTING"}, rule...)
	if full[0] != "-t" || full[2] != "-A" || full[3] != "POSTROUTING" {
		t.Fatalf("unexpected full args %v", full)
	}
}

func TestDNATRuleArgs(t *testing.T) {
	t.Parallel()
	r := FCDNATRule{Proto: "tcp", HostPort: 18080, GuestPort: 8080, GuestIP: "10.77.3.2"}
	args := DNATRuleArgs(r)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "127.0.0.1") || !strings.Contains(joined, "18080") {
		t.Fatalf("host match missing: %v", args)
	}
	if !strings.Contains(joined, "DNAT") || !strings.Contains(joined, "10.77.3.2:8080") {
		t.Fatalf("destination missing: %v", args)
	}
	// OUTPUT install shape.
	out := append([]string{"-t", "nat", "-A", "OUTPUT"}, args...)
	if !strings.Contains(strings.Join(out, " "), "--to-destination 10.77.3.2:8080") {
		t.Fatalf("OUTPUT DNAT shape: %v", out)
	}
}

//go:build linux

package hypervisor

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/cxdy/grain/internal/vm"
)

// runIP runs `ip` with args. Overridable in tests.
var runIP = func(args ...string) error {
	cmd := exec.Command("ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runIPTables runs iptables. Overridable in tests.
var runIPTables = func(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runSysctl writes a sysctl. Overridable in tests.
var runSysctl = func(key, val string) error {
	cmd := exec.Command("sysctl", "-w", key+"="+val)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sysctl %s=%s: %v (%s)", key, val, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetupFCNet creates the TAP, addresses it, enables forwarding/MASQUERADE,
// host→guest SNAT (for loopback clients), and applies DNAT publishes.
// Returns state for persistence and cleanup. Requires CAP_NET_ADMIN.
func SetupFCNet(plan FCNetPlan, hostSSH, hostAgent int, fwds []vm.PortForward) (FCNetState, error) {
	st := FCNetState{FCNetPlan: plan}
	if err := createAndAddrTAP(plan); err != nil {
		return st, PrivilegeErrorHint("create tap", err)
	}
	if err := enableForwarding(); err != nil {
		_ = deleteTAP(plan.TapName)
		return st, PrivilegeErrorHint("enable ip_forward", err)
	}
	if err := ensureMASQUERADE(plan); err != nil {
		_ = deleteTAP(plan.TapName)
		return st, PrivilegeErrorHint("masquerade", err)
	}
	// SNAT loopback-origin DNATed packets to HostIP so the guest can reply.
	if err := ensureHostToGuestSNAT(plan); err != nil {
		_ = deleteTAP(plan.TapName)
		return st, PrivilegeErrorHint("host-to-guest snat", err)
	}
	if err := ensureForwardFilter(plan); err != nil {
		_ = deleteTAP(plan.TapName)
		return st, PrivilegeErrorHint("forward filter", err)
	}
	rules := BuildDNATRules(plan.GuestIP, hostSSH, hostAgent, fwds)
	for _, r := range rules {
		if err := applyDNAT(r); err != nil {
			// best-effort rollback
			_ = TeardownFCNet(FCNetState{FCNetPlan: plan, DNATRules: rules[:len(st.DNATRules)]})
			_ = deleteTAP(plan.TapName)
			return st, PrivilegeErrorHint("dnat publish", err)
		}
		st.DNATRules = append(st.DNATRules, r)
	}
	return st, nil
}

// TeardownFCNet removes DNAT rules, host→guest SNAT, and the TAP device.
func TeardownFCNet(st FCNetState) error {
	var first error
	for i := len(st.DNATRules) - 1; i >= 0; i-- {
		if err := removeDNAT(st.DNATRules[i]); err != nil && first == nil {
			first = err
		}
	}
	if err := removeHostToGuestSNAT(st.FCNetPlan); err != nil && first == nil {
		first = err
	}
	if st.TapName != "" {
		if err := deleteTAP(st.TapName); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func createAndAddrTAP(plan FCNetPlan) error {
	// Delete stale device with same name.
	_ = runIP("link", "del", plan.TapName)
	if err := runIP("tuntap", "add", "dev", plan.TapName, "mode", "tap"); err != nil {
		// Older iproute2: ip tuntap add mode tap name DEV
		if err2 := runIP("tuntap", "add", "mode", "tap", "name", plan.TapName); err2 != nil {
			return err
		}
	}
	if err := runIP("addr", "add", fmt.Sprintf("%s/%d", plan.HostIP, plan.Prefix), "dev", plan.TapName); err != nil {
		_ = deleteTAP(plan.TapName)
		return err
	}
	if err := runIP("link", "set", "dev", plan.TapName, "up"); err != nil {
		_ = deleteTAP(plan.TapName)
		return err
	}
	return nil
}

func deleteTAP(name string) error {
	if name == "" {
		return nil
	}
	return runIP("link", "del", name)
}

func enableForwarding() error {
	return runSysctl("net.ipv4.ip_forward", "1")
}

func ensureMASQUERADE(plan FCNetPlan) error {
	// Egress from guest subnet.
	src := fmt.Sprintf("10.%d.%d.0/%d", fcNetBaseOctet, plan.Slot, plan.Prefix)
	// Idempotent: check exists then add.
	check := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-s", src, "!", "-o", plan.TapName, "-j", "MASQUERADE")
	if check.Run() == nil {
		return nil
	}
	return runIPTables("-t", "nat", "-A", "POSTROUTING", "-s", src, "!", "-o", plan.TapName, "-j", "MASQUERADE")
}

// ensureHostToGuestSNAT rewrites DNATed local-client packets so saddr is the
// TAP host IP. Without this, guest replies to 127.0.0.1 never leave the guest.
func ensureHostToGuestSNAT(plan FCNetPlan) error {
	if plan.TapName == "" || plan.HostIP == "" {
		return fmt.Errorf("empty tap or host IP for SNAT")
	}
	rule := HostToGuestSNATRule(plan)
	checkArgs := append([]string{"-t", "nat", "-C", "POSTROUTING"}, rule...)
	if exec.Command("iptables", checkArgs...).Run() == nil {
		return nil
	}
	addArgs := append([]string{"-t", "nat", "-A", "POSTROUTING"}, rule...)
	return runIPTables(addArgs...)
}

func removeHostToGuestSNAT(plan FCNetPlan) error {
	if plan.TapName == "" || plan.HostIP == "" {
		return nil
	}
	rule := HostToGuestSNATRule(plan)
	delArgs := append([]string{"-t", "nat", "-D", "POSTROUTING"}, rule...)
	// Best-effort: rule may already be gone.
	_ = runIPTables(delArgs...)
	return nil
}

func ensureForwardFilter(plan FCNetPlan) error {
	// Allow traffic to/from the TAP.
	for _, args := range [][]string{
		{"-C", "FORWARD", "-i", plan.TapName, "-j", "ACCEPT"},
		{"-C", "FORWARD", "-o", plan.TapName, "-j", "ACCEPT"},
	} {
		if exec.Command("iptables", args...).Run() != nil {
			// replace -C with -A
			a := append([]string{}, args...)
			a[0] = "-A"
			if err := runIPTables(a...); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyDNAT(r FCDNATRule) error {
	// OUTPUT chain for local (loopback) clients; PREROUTING for remote (rare for grain).
	spec := DNATRuleArgs(r)
	for _, chain := range []string{"OUTPUT", "PREROUTING"} {
		args := append([]string{"-t", "nat", "-A", chain}, spec...)
		if err := runIPTables(args...); err != nil {
			return err
		}
	}
	// Allow forwarded DNAT traffic.
	fwd := []string{
		"-A", "FORWARD",
		"-p", r.Proto,
		"-d", r.GuestIP,
		"--dport", fmt.Sprintf("%d", r.GuestPort),
		"-j", "ACCEPT",
	}
	_ = runIPTables(fwd...) // best-effort
	return nil
}

func removeDNAT(r FCDNATRule) error {
	var first error
	spec := DNATRuleArgs(r)
	for _, chain := range []string{"OUTPUT", "PREROUTING"} {
		args := append([]string{"-t", "nat", "-D", chain}, spec...)
		if err := runIPTables(args...); err != nil && first == nil {
			first = err
		}
	}
	return first
}

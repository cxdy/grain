//go:build linux

package hypervisor

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func withMockIPHelpers(t *testing.T) {
	t.Helper()
	oldIP, oldIPT, oldSys := runIP, runIPTables, runSysctl
	t.Cleanup(func() {
		runIP, runIPTables, runSysctl = oldIP, oldIPT, oldSys
	})
	runIP = func(args ...string) error { return nil }
	runIPTables = func(args ...string) error { return nil }
	runSysctl = func(key, val string) error { return nil }
}

func TestSetupAndTeardownFCNetSuccess(t *testing.T) {
	withMockIPHelpers(t)
	plan := PlanFCNet("unit-fcnet")
	st, err := SetupFCNet(plan, 2200, 7700, []vm.PortForward{
		{HostPort: 8080, GuestPort: 80, Proto: "tcp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st.TapName != plan.TapName || len(st.DNATRules) == 0 {
		t.Fatalf("%+v", st)
	}
	if err := TeardownFCNet(st); err != nil {
		// removeDNAT may return first error from mock — we return nil always
		t.Log(err)
	}
}

func TestSetupFCNetErrorBranches(t *testing.T) {
	plan := PlanFCNet("err-fcnet")

	// create TAP fails
	oldIP, oldIPT, oldSys := runIP, runIPTables, runSysctl
	t.Cleanup(func() {
		runIP, runIPTables, runSysctl = oldIP, oldIPT, oldSys
	})
	runIP = func(args ...string) error {
		return errors.New("permission denied")
	}
	runIPTables = func(args ...string) error { return nil }
	runSysctl = func(key, val string) error { return nil }
	if _, err := SetupFCNet(plan, 0, 0, nil); err == nil {
		t.Fatal("want tap error")
	}

	// TAP ok, sysctl fails
	runIP = func(args ...string) error { return nil }
	runSysctl = func(key, val string) error { return errors.New("sysctl fail") }
	if _, err := SetupFCNet(plan, 0, 0, nil); err == nil {
		t.Fatal("want forward error")
	}

	// MASQUERADE fails
	runSysctl = func(key, val string) error { return nil }
	runIPTables = func(args ...string) error {
		if strings.Contains(strings.Join(args, " "), "MASQUERADE") {
			return errors.New("masq fail")
		}
		return nil
	}
	if _, err := SetupFCNet(plan, 0, 0, nil); err == nil {
		t.Fatal("want masq error")
	}

	// SNAT fails
	runIPTables = func(args ...string) error {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "SNAT") {
			return errors.New("snat fail")
		}
		return nil
	}
	if _, err := SetupFCNet(plan, 0, 0, nil); err == nil {
		t.Fatal("want snat error")
	}

	// forward filter fails
	runIPTables = func(args ...string) error {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "FORWARD") && strings.Contains(joined, "-A") {
			return errors.New("fwd fail")
		}
		return nil
	}
	if _, err := SetupFCNet(plan, 0, 0, nil); err == nil {
		t.Fatal("want forward filter error")
	}

	// DNAT fails after partial rules
	var dnatCalls int
	runIPTables = func(args ...string) error {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "DNAT") {
			dnatCalls++
			if dnatCalls > 1 {
				return errors.New("dnat fail")
			}
		}
		return nil
	}
	if _, err := SetupFCNet(plan, 2200, 0, nil); err == nil {
		t.Fatal("want dnat error")
	}
}

func TestCreateAndAddrTAPFallback(t *testing.T) {
	old := runIP
	t.Cleanup(func() { runIP = old })
	var calls []string
	runIP = func(args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		// first tuntap add form fails; second succeeds
		if len(args) >= 2 && args[0] == "tuntap" && args[1] == "add" && args[2] == "dev" {
			return errors.New("old style")
		}
		return nil
	}
	plan := PlanFCNet("tap-fallback")
	if err := createAndAddrTAP(plan); err != nil {
		t.Fatal(err)
	}
	// both forms attempted
	foundFallback := false
	for _, c := range calls {
		if strings.Contains(c, "name "+plan.TapName) {
			foundFallback = true
		}
	}
	if !foundFallback {
		t.Fatalf("calls=%v", calls)
	}

	// addr add fails → delete TAP
	runIP = func(args ...string) error {
		if len(args) >= 1 && args[0] == "addr" {
			return errors.New("addr fail")
		}
		return nil
	}
	if err := createAndAddrTAP(plan); err == nil {
		t.Fatal("want addr error")
	}

	// link set up fails
	runIP = func(args ...string) error {
		if len(args) >= 2 && args[0] == "link" && args[1] == "set" {
			return errors.New("up fail")
		}
		return nil
	}
	if err := createAndAddrTAP(plan); err == nil {
		t.Fatal("want up error")
	}
}

func TestDeleteTAPEmptyAndHelpers(t *testing.T) {
	oldIP, oldIPT, oldSys := runIP, runIPTables, runSysctl
	t.Cleanup(func() {
		runIP, runIPTables, runSysctl = oldIP, oldIPT, oldSys
	})
	runIP = func(args ...string) error { return nil }
	runIPTables = func(args ...string) error { return nil }
	runSysctl = func(key, val string) error { return nil }

	if err := deleteTAP(""); err != nil {
		t.Fatal(err)
	}
	if err := enableForwarding(); err != nil {
		t.Fatal(err)
	}
	plan := PlanFCNet("helpers")
	if guestSubnetCIDR(plan) == "" {
		t.Fatal("cidr")
	}
	if err := ensureMASQUERADE(plan); err != nil {
		t.Fatal(err)
	}
	removeMASQUERADE(plan)
	removeMASQUERADE(FCNetPlan{}) // empty tap early return
	removeForwardFilter(plan)
	removeForwardFilter(FCNetPlan{})
	if err := ensureHostToGuestSNAT(plan); err != nil {
		t.Fatal(err)
	}
	if err := ensureHostToGuestSNAT(FCNetPlan{}); err == nil {
		t.Fatal("empty plan snat")
	}
	removeHostToGuestSNAT(plan)
	removeHostToGuestSNAT(FCNetPlan{})
	if err := ensureForwardFilter(plan); err != nil {
		t.Fatal(err)
	}

	rules := BuildDNATRules(plan.GuestIP, 22, 7475, []vm.PortForward{{HostPort: 1, GuestPort: 2}})
	for _, r := range rules {
		if err := applyDNAT(r); err != nil {
			t.Fatal(err)
		}
		_ = removeDNAT(r)
	}
}

func TestTeardownFCNetCollectsErrors(t *testing.T) {
	oldIPT, oldIP := runIPTables, runIP
	t.Cleanup(func() {
		runIPTables, runIP = oldIPT, oldIP
	})
	runIPTables = func(args ...string) error {
		return fmt.Errorf("ipt fail")
	}
	runIP = func(args ...string) error {
		return fmt.Errorf("ip fail")
	}
	plan := PlanFCNet("teardown-err")
	st := FCNetState{
		FCNetPlan: plan,
		DNATRules: []FCDNATRule{{Proto: "tcp", HostPort: 1, GuestPort: 2, GuestIP: plan.GuestIP}},
	}
	err := TeardownFCNet(st)
	if err == nil {
		t.Fatal("want error")
	}
}

func TestApplyDNATError(t *testing.T) {
	old := runIPTables
	t.Cleanup(func() { runIPTables = old })
	runIPTables = func(args ...string) error {
		return errors.New("nope")
	}
	if err := applyDNAT(FCDNATRule{Proto: "tcp", HostPort: 1, GuestPort: 2, GuestIP: "10.0.0.2"}); err == nil {
		t.Fatal("want error")
	}
}

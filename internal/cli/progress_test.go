package cli

import (
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func TestCreateStage(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "disk"},
		{time.Second, "disk"},
		{3 * time.Second, "boot"},
		{10 * time.Second, "waiting ssh"},
		{2 * time.Minute, "waiting ssh"},
	}
	for _, tc := range cases {
		if got := createStage(tc.d); got != tc.want {
			t.Errorf("createStage(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestPhaseLabel(t *testing.T) {
	cases := map[string]string{
		vm.PhaseImage:     "image",
		vm.PhaseDisk:      "disk",
		vm.PhaseSeed:      "seed",
		vm.PhaseQEMU:      "boot",
		vm.PhaseWaitSSH:   "waiting ssh",
		vm.PhaseWaitAgent: "waiting agent",
		vm.PhaseUserdata:  "waiting userdata",
		vm.PhaseReady:     "ready",
		vm.PhaseError:     "error",
		"":                "starting",
	}
	for phase, want := range cases {
		if got := phaseLabel(phase, ""); got != want {
			t.Errorf("phaseLabel(%q)=%q want %q", phase, got, want)
		}
	}
	if got := phaseLabel(vm.PhaseWaitSSH, "waiting for ssh (agent deploy)"); got != "waiting agent via ssh" {
		t.Errorf("agent deploy label: %q", got)
	}
}

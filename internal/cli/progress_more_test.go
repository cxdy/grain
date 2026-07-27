package cli

import (
	"os"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func TestPrintCreateProgress(t *testing.T) {
	// Non-TTY style lines — just ensure no panic.
	printCreateProgress(false, "", "creating", "disk", 2*time.Second)
	printCreateProgress(true, "⠋", "creating", "boot", time.Second)
}

func TestPhaseLabelUnknown(t *testing.T) {
	t.Parallel()
	if got := phaseLabel("custom-phase"); got != "custom-phase" {
		t.Fatalf("%q", got)
	}
}

func TestCreateProgressStop(t *testing.T) {
	stop := createProgress("testing")
	time.Sleep(50 * time.Millisecond)
	stop()
	// double-stop should be safe
	stop()
}

func TestCreateProgressDefaultLabel(t *testing.T) {
	stop := createProgress("")
	time.Sleep(20 * time.Millisecond)
	stop()
}

func TestCreateProgressEvents(t *testing.T) {
	onEvent, stop := createProgressEvents("evt")
	onEvent(vm.CreateEvent{Phase: vm.PhaseDisk})
	onEvent(vm.CreateEvent{Phase: vm.PhaseQEMU})
	onEvent(vm.CreateEvent{Phase: vm.PhaseReady})
	time.Sleep(50 * time.Millisecond)
	stop()
	stop() // idempotent
}

func TestCreateProgressEventsDefaultLabel(t *testing.T) {
	onEvent, stop := createProgressEvents("")
	onEvent(vm.CreateEvent{Phase: ""})
	stop()
}

func TestIsTerminal(t *testing.T) {
	// os.Stderr may or may not be a TTY under go test; just call it.
	_ = isTerminal(os.Stderr)
	// regular file is not a terminal
	f, err := os.CreateTemp(t.TempDir(), "f")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Fatal("regular file should not be a terminal")
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

// ---- from progress_test.go ----

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

// ---- from progress_more_test.go ----

func TestPrintCreateProgress(t *testing.T) {
	// Non-TTY style lines — just ensure no panic.
	printCreateProgress(false, "", "creating", "disk", 2*time.Second)
	printCreateProgress(true, "⠋", "creating", "boot", time.Second)
}

func TestPhaseLabelUnknown(t *testing.T) {
	t.Parallel()
	if got := phaseLabel("custom-phase", ""); got != "custom-phase" {
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

func TestFormatByteCount(t *testing.T) {
	t.Parallel()
	cases := map[int64]string{
		0:    "0B",
		512:  "512B",
		1024: "1.0KiB",
	}
	for n, want := range cases {
		if got := formatByteCount(n); got != want {
			t.Errorf("formatByteCount(%d)=%q want %q", n, got, want)
		}
	}
}

func TestCountingReaderWriter(t *testing.T) {
	t.Parallel()
	var got int
	r := &countingReader{r: strings.NewReader("hello"), onRead: func(n int) { got += n }}
	buf := make([]byte, 8)
	n, err := r.Read(buf)
	if err != nil || n != 5 || got != 5 {
		t.Fatalf("read n=%d err=%v got=%d", n, err, got)
	}
	var wgot int
	var out strings.Builder
	w := &countingWriter{w: &out, onWrite: func(n int) { wgot += n }}
	n, err = w.Write([]byte("ab"))
	if err != nil || n != 2 || wgot != 2 || out.String() != "ab" {
		t.Fatalf("write n=%d err=%v wgot=%d out=%q", n, err, wgot, out.String())
	}
}

func TestTransferProgressFinish(t *testing.T) {
	p := newTransferProgress("cp")
	p.SetDetail("put file")
	p.SetBytes(10, 100)
	p.AddBytes(5)
	p.Finish("cp: done")
	// second Finish is a no-op
	p.Finish("again")
}

func TestHostTreeSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := hostTreeSize(dir)
	if err != nil || n != 5 {
		t.Fatalf("size=%d err=%v", n, err)
	}
	n, err = hostTreeSize(filepath.Join(dir, "a"))
	if err != nil || n != 5 {
		t.Fatalf("file size=%d err=%v", n, err)
	}
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

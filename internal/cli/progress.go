package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

// createProgress prints a live status line with time-based stage heuristics.
// Prefer createProgressEvents when streaming phases from the daemon.
func createProgress(label string) (stop func()) {
	if label == "" {
		label = "creating"
	}

	done := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	start := time.Now()
	tty := isTerminal(os.Stderr)

	go func() {
		defer close(finished)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		lastStage := ""
		interval := 80 * time.Millisecond
		if !tty {
			interval = 400 * time.Millisecond
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		lastStage = createStage(0)
		printCreateProgress(tty, frames[0], label, lastStage, 0)
		for {
			select {
			case <-done:
				return
			case <-t.C:
				elapsed := time.Since(start)
				stage := createStage(elapsed)
				if tty {
					printCreateProgress(true, frames[i%len(frames)], label, stage, elapsed)
					i++
					continue
				}
				if stage != lastStage {
					printCreateProgress(false, "", label, stage, elapsed)
					lastStage = stage
				}
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
			<-finished
			if tty {
				_, _ = fmt.Fprint(os.Stderr, "\r\033[K")
			}
		})
	}
}

// createProgressEvents starts a TTY spinner driven by CreateEvent phases.
// Call onEvent for each event; call stop when finished (clears the line).
func createProgressEvents(label string) (onEvent func(vm.CreateEvent), stop func()) {
	if label == "" {
		label = "creating"
	}

	done := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	start := time.Now()
	tty := isTerminal(os.Stderr)

	var mu sync.Mutex
	stage := "starting"

	go func() {
		defer close(finished)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		lastPrinted := ""
		interval := 80 * time.Millisecond
		if !tty {
			interval = 400 * time.Millisecond
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		printCreateProgress(tty, frames[0], label, stage, 0)
		lastPrinted = stage
		for {
			select {
			case <-done:
				return
			case <-t.C:
				mu.Lock()
				cur := stage
				mu.Unlock()
				elapsed := time.Since(start)
				if tty {
					printCreateProgress(true, frames[i%len(frames)], label, cur, elapsed)
					i++
					continue
				}
				if cur != lastPrinted {
					printCreateProgress(false, "", label, cur, elapsed)
					lastPrinted = cur
				}
			}
		}
	}()

	onEvent = func(ev vm.CreateEvent) {
		mu.Lock()
		stage = phaseLabel(ev.Phase, ev.Message)
		mu.Unlock()
	}

	stop = func() {
		once.Do(func() {
			close(done)
			<-finished
			if tty {
				_, _ = fmt.Fprint(os.Stderr, "\r\033[K")
			}
		})
	}
	return onEvent, stop
}

func printCreateProgress(tty bool, frame, label, stage string, elapsed time.Duration) {
	e := elapsed.Round(time.Second)
	if tty {
		_, _ = fmt.Fprintf(os.Stderr, "\r\033[K  %s %s  %-12s %s", frame, label, stage, e)
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s: %s (%s)\n", label, stage, e)
}

// phaseLabel maps CreateEvent phases to short spinner labels.
func phaseLabel(phase, message string) string {
	switch phase {
	case vm.PhaseImage:
		return "image"
	case vm.PhaseDisk:
		return "disk"
	case vm.PhaseSeed:
		return "seed"
	case vm.PhaseQEMU:
		return "boot"
	case vm.PhaseWaitSSH:
		// Soft agent-deploy fallback after baked-agent probe (not pure SSH wait).
		if strings.Contains(strings.ToLower(message), "agent deploy") {
			return "waiting agent via ssh"
		}
		return "waiting ssh"
	case vm.PhaseWaitAgent:
		return "waiting agent"
	case vm.PhaseUserdata:
		return "waiting userdata"
	case vm.PhaseReady:
		return "ready"
	case vm.PhaseError:
		return "error"
	default:
		if phase == "" {
			return "starting"
		}
		return phase
	}
}

// createStage is a time-heuristic fallback when events are unavailable.
func createStage(d time.Duration) string {
	switch {
	case d < 2*time.Second:
		return "disk"
	case d < 8*time.Second:
		return "boot"
	default:
		return "waiting ssh"
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

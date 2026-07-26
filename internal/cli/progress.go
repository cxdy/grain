package cli

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// createProgress prints a live status line while Create blocks on disk/boot/SSH.
// Call the returned stop func when done; it clears the line on a TTY.
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
		// faster tick on TTY for smooth spinner; slower off-TTY for stage lines
		interval := 80 * time.Millisecond
		if !tty {
			interval = 400 * time.Millisecond
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		// first paint immediately
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
				// clear spinner line so the next printf starts clean
				fmt.Fprint(os.Stderr, "\r\033[K")
			}
		})
	}
}

func printCreateProgress(tty bool, frame, label, stage string, elapsed time.Duration) {
	e := elapsed.Round(time.Second)
	if tty {
		fmt.Fprintf(os.Stderr, "\r\033[K  %s %s  %-12s %s", frame, label, stage, e)
		return
	}
	fmt.Fprintf(os.Stderr, "%s: %s (%s)\n", label, stage, e)
}

// createStage is heuristic (API is one blocking call) but tracks the real wait phases.
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

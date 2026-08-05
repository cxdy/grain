package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	case vm.PhaseBootstrap:
		// Prefer guest-authored message (phase + human text).
		if message != "" {
			return message
		}
		return "waiting bootstrap"
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

// transferProgress prints live transfer status for cp/sync (stderr).
// TTY: spinner + rewriting line. Non-TTY: line when stage text changes.
type transferProgress struct {
	label string
	tty   bool
	start time.Time

	mu     sync.Mutex
	detail string // current stage text (action, path, counts, …)
	done   int64  // bytes transferred (optional)
	total  int64  // total bytes when known (0 = unknown)

	stopOnce sync.Once
	stopCh   chan struct{}
	finished chan struct{}
}

func newTransferProgress(label string) *transferProgress {
	if label == "" {
		label = "transfer"
	}
	p := &transferProgress{
		label:    label,
		tty:      isTerminal(os.Stderr),
		start:    time.Now(),
		stopCh:   make(chan struct{}),
		finished: make(chan struct{}),
		detail:   "starting",
	}
	go p.loop()
	return p
}

func (p *transferProgress) loop() {
	defer close(p.finished)
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0
	interval := 80 * time.Millisecond
	if !p.tty {
		interval = 500 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	lastLine := ""
	p.render(frames[0])
	lastLine = p.line()
	for {
		select {
		case <-p.stopCh:
			return
		case <-t.C:
			if p.tty {
				p.render(frames[i%len(frames)])
				i++
				continue
			}
			cur := p.line()
			if cur != lastLine {
				_, _ = fmt.Fprintln(os.Stderr, cur)
				lastLine = cur
			}
		}
	}
}

// SetDetail sets the human status text (e.g. "put 3/12 src/main.go").
func (p *transferProgress) SetDetail(detail string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.detail = detail
	p.mu.Unlock()
}

// SetBytes updates byte counters (total 0 = unknown).
func (p *transferProgress) SetBytes(done, total int64) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.done = done
	p.total = total
	p.mu.Unlock()
}

// AddBytes increments the done counter (for streaming).
func (p *transferProgress) AddBytes(n int64) {
	if p == nil || n == 0 {
		return
	}
	p.mu.Lock()
	p.done += n
	p.mu.Unlock()
}

func (p *transferProgress) line() string {
	p.mu.Lock()
	detail := p.detail
	done, total := p.done, p.total
	p.mu.Unlock()
	elapsed := time.Since(p.start).Round(time.Second)
	var b strings.Builder
	b.WriteString(p.label)
	if detail != "" {
		b.WriteString("  ")
		b.WriteString(detail)
	}
	if done > 0 || total > 0 {
		b.WriteString("  ")
		b.WriteString(formatByteCount(done))
		if total > 0 {
			b.WriteByte('/')
			b.WriteString(formatByteCount(total))
			if total > 0 {
				pct := int(done * 100 / total)
				if pct > 100 {
					pct = 100
				}
				b.WriteString(fmt.Sprintf(" %d%%", pct))
			}
		}
	}
	b.WriteString("  ")
	b.WriteString(elapsed.String())
	return b.String()
}

func (p *transferProgress) render(frame string) {
	if !p.tty {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "\r\033[K  %s %s", frame, p.line())
}

// Finish stops the spinner and prints a final summary line (non-empty).
func (p *transferProgress) Finish(summary string) {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		close(p.stopCh)
		<-p.finished
		if p.tty {
			_, _ = fmt.Fprint(os.Stderr, "\r\033[K")
		}
		if summary != "" {
			_, _ = fmt.Fprintln(os.Stderr, summary)
		}
	})
}

func formatByteCount(n int64) string {
	if n < 0 {
		n = 0
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// countingReader wraps r, reporting bytes via onRead (may be called often).
type countingReader struct {
	r      io.Reader
	onRead func(n int)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(n)
	}
	return n, err
}

// countingWriter wraps w, reporting bytes via onWrite.
type countingWriter struct {
	w       io.Writer
	onWrite func(n int)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
	return n, err
}

// hostTreeSize sums regular file sizes under path (file or directory).
func hostTreeSize(path string) (int64, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !fi.IsDir() {
		if fi.Mode().IsRegular() {
			return fi.Size(), nil
		}
		return 0, nil
	}
	var total int64
	err = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

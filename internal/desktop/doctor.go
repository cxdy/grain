package desktop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/hostbin"
)

// DoctorCheck is one host/daemon diagnostic row for the UI.
type DoctorCheck struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
	Command string `json:"command,omitempty"`
}

// RunDoctorChecks performs local host checks for the Desktop Doctor page.
// Remote connections still get host-side tool checks; daemon health is separate.
func RunDoctorChecks(ctx context.Context, cfg Config, svc *Service) []DoctorCheck {
	if ctx == nil {
		ctx = context.Background()
	}
	var out []DoctorCheck

	// Data dir
	dir := expandHome(cfg.DataDir)
	if dir == "" {
		out = append(out, DoctorCheck{Name: "data dir", OK: false, Message: "data_dir empty", Fix: "Set data_dir in config.yaml"})
	} else if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		out = append(out, DoctorCheck{
			Name: "data dir", OK: false, Message: fmt.Sprintf("%s missing", dir),
			Fix: "Create the directory or fix data_dir", Command: fmt.Sprintf("mkdir -p %q", dir),
		})
	} else {
		out = append(out, DoctorCheck{Name: "data dir", OK: true, Message: dir})
	}

	// grain CLI
	if p, err := exec.LookPath("grain"); err != nil {
		out = append(out, DoctorCheck{
			Name: "grain CLI", OK: false, Message: "not on PATH",
			Fix: "Install grain and ensure it is on PATH", Command: "curl -fsSL https://grainvm.com/install.sh | bash",
		})
	} else {
		out = append(out, DoctorCheck{Name: "grain CLI", OK: true, Message: p})
	}

	// qemu-img
	if p, err := hostbin.LookPath("qemu-img"); err != nil {
		out = append(out, DoctorCheck{
			Name: "qemu-img", OK: false, Message: "not found",
			Fix: "Install QEMU tools", Command: "brew install qemu",
		})
	} else {
		out = append(out, DoctorCheck{Name: "qemu-img", OK: true, Message: p})
	}

	// qemu system binary
	qemu := "qemu-system-x86_64"
	if runtime.GOARCH == "arm64" {
		qemu = "qemu-system-aarch64"
	}
	if p, err := hostbin.LookPath(qemu); err != nil {
		out = append(out, DoctorCheck{
			Name: qemu, OK: false, Message: "not found",
			Fix: "Install QEMU", Command: "brew install qemu",
		})
	} else {
		out = append(out, DoctorCheck{Name: qemu, OK: true, Message: p})
	}

	// Socket / API reachability via service health when available
	if svc != nil {
		hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		hs, err := svc.Health(hctx)
		cancel()
		if err != nil {
			out = append(out, DoctorCheck{
				Name: "daemon health", OK: false, Message: err.Error(),
				Fix: "Start the local daemon", Command: "grain up",
			})
		} else if !hs.Healthy {
			out = append(out, DoctorCheck{
				Name: "daemon health", OK: false, Message: hs.Message,
				Fix: "Start or fix the daemon for the active host", Command: "grain up",
			})
		} else {
			msg := "remote · " + hs.Connection
			if hs.Local {
				msg = "local · " + hs.Connection
			}
			out = append(out, DoctorCheck{Name: "daemon health", OK: true, Message: msg})
		}
	}

	// Default image ready (soft)
	if cfg.Image != "" && cfg.DataDir != "" {
		ready := false
		for _, img := range ListImages(cfg.DataDir) {
			if img.ID == cfg.Image && img.Ready {
				ready = true
				break
			}
		}
		if ready {
			out = append(out, DoctorCheck{Name: "default image", OK: true, Message: cfg.Image + " ready"})
		} else {
			out = append(out, DoctorCheck{
				Name: "default image", OK: false, Message: cfg.Image + " not on disk",
				Fix:     "Pull the image from Desktop → Images or CLI",
				Command: fmt.Sprintf("grain image pull %s", cfg.Image),
			})
		}
	}

	// OS note
	out = append(out, DoctorCheck{
		Name: "host OS", OK: true,
		Message: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	})

	// Socket path exists (local)
	sock := expandHome(cfg.Socket)
	if sock != "" {
		if _, err := os.Stat(sock); err == nil {
			out = append(out, DoctorCheck{Name: "unix socket", OK: true, Message: sock})
		} else {
			out = append(out, DoctorCheck{
				Name: "unix socket", OK: false, Message: "not present (daemon may use TCP only)",
				Fix: "Run grain up, or connect via api_url", Command: "grain up",
			})
		}
	}

	_ = filepath.Separator // keep path import used on all platforms
	return out
}

// DoctorSummary counts pass/fail for UI badges.
func DoctorSummary(checks []DoctorCheck) (pass, fail int) {
	for _, c := range checks {
		if c.OK {
			pass++
		} else {
			fail++
		}
	}
	return pass, fail
}

// RepairResult is output from a safe Doctor repair command.
type RepairResult struct {
	Command string `json:"command"`
	OK      bool   `json:"ok"`
	Output  string `json:"output"`
}

// allowedRepairCommands are the only commands Doctor may execute.
var allowedRepairCommands = map[string][]string{
	"grain up":       {"up"},
	"grain up --mcp": {"up", "--mcp"},
}

// DoctorRepair runs an allowlisted repair command via the grain binary on PATH.
func DoctorRepair(ctx context.Context, command string, runner CommandRunner) (RepairResult, error) {
	cmd := strings.TrimSpace(command)
	args, ok := allowedRepairCommands[cmd]
	if !ok {
		// Also accept bare forms without "grain " prefix
		if a, ok2 := allowedRepairCommands["grain "+cmd]; ok2 {
			args = a
			cmd = "grain " + cmd
			ok = true
		}
	}
	if !ok {
		return RepairResult{Command: cmd}, fmt.Errorf("repair not allowed for %q (only: grain up, grain up --mcp)", command)
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	grain, err := runner.LookPath("grain")
	if err != nil {
		return RepairResult{Command: cmd}, fmt.Errorf("grain not on PATH")
	}
	// Use StartBackground for long-lived grain up; capture is limited — run via exec for output.
	out, err := exec.CommandContext(ctx, grain, args...).CombinedOutput()
	res := RepairResult{Command: cmd, Output: strings.TrimSpace(string(out))}
	if err != nil {
		res.OK = false
		if res.Output == "" {
			res.Output = err.Error()
		}
		return res, fmt.Errorf("%s: %w", res.Output, err)
	}
	res.OK = true
	return res, nil
}

package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// CommandRunner runs or starts external processes (injectable for tests).
type CommandRunner interface {
	// LookPath resolves a binary on PATH (like exec.LookPath).
	LookPath(file string) (string, error)
	// StartBackground starts name with args and does not wait for exit.
	StartBackground(ctx context.Context, name string, args ...string) error
}

// ExecRunner is the production CommandRunner using os/exec.
type ExecRunner struct{}

// LookPath implements CommandRunner.
func (ExecRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// StartBackground implements CommandRunner.
func (ExecRunner) StartBackground(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	// Detach from parent's lifetime for daemon-style processes when possible.
	if err := cmd.Start(); err != nil {
		return err
	}
	// Do not cmd.Wait() — grain up backgrounds itself; reaping is best-effort.
	go func() { _ = cmd.Wait() }()
	return nil
}

// ShouldStartLocalDaemon is the pure decision matrix for splash auto-start.
// Remote connections never start a daemon. Local + unhealthy + pref => start.
func ShouldStartLocalDaemon(conn Connection, daemonHealthy bool, startLocalPref bool) bool {
	if !conn.IsLocal() {
		return false
	}
	if daemonHealthy {
		return false
	}
	return startLocalPref
}

// StartDaemonResult is the outcome of EnsureLocalDaemon.
type StartDaemonResult struct {
	// Started is true when grain up was invoked.
	Started bool
	// AlreadyHealthy is true when no start was needed.
	AlreadyHealthy bool
	// Message is a human-readable status for the splash UI.
	Message string
	// GrainPath is the resolved binary when Started.
	GrainPath string
}

// HealthFunc checks daemon health.
type HealthFunc func(ctx context.Context) error

// SleepFunc is injectable time.Sleep.
type SleepFunc func(d time.Duration)

// EnsureLocalDaemon starts the system `grain up` when the local daemon is down
// and waits until healthy or timeout. Remote connections return an error if
// unhealthy (never start remote engines).
func EnsureLocalDaemon(
	ctx context.Context,
	conn Connection,
	startLocalPref bool,
	health HealthFunc,
	runner CommandRunner,
	sleep SleepFunc,
	wait time.Duration,
	poll time.Duration,
) (StartDaemonResult, error) {
	if sleep == nil {
		sleep = time.Sleep
	}
	if poll <= 0 {
		poll = 200 * time.Millisecond
	}
	if wait <= 0 {
		wait = 30 * time.Second
	}
	if health == nil {
		return StartDaemonResult{}, fmt.Errorf("health check is required")
	}
	if runner == nil {
		runner = ExecRunner{}
	}

	err := health(ctx)
	if err == nil {
		return StartDaemonResult{
			AlreadyHealthy: true,
			Message:        "daemon healthy",
		}, nil
	}

	if !conn.IsLocal() {
		return StartDaemonResult{
			Message: "remote daemon unreachable",
		}, fmt.Errorf("remote connection %q is not healthy: %w", conn.Name, err)
	}

	if !startLocalPref {
		return StartDaemonResult{
			Message: "local daemon not running (auto-start disabled)",
		}, fmt.Errorf("local daemon not healthy: %w", err)
	}

	grainPath, lookErr := runner.LookPath("grain")
	if lookErr != nil {
		return StartDaemonResult{
			Message: "grain binary not found on PATH",
		}, fmt.Errorf("find grain: %w", lookErr)
	}

	if startErr := runner.StartBackground(ctx, grainPath, "up"); startErr != nil {
		return StartDaemonResult{
			Message:   "failed to start grain up",
			GrainPath: grainPath,
		}, fmt.Errorf("grain up: %w", startErr)
	}

	deadline := time.Now().Add(wait)
	for {
		if ctx.Err() != nil {
			return StartDaemonResult{
				Started:   true,
				Message:   "timed out waiting for daemon",
				GrainPath: grainPath,
			}, ctx.Err()
		}
		if time.Now().After(deadline) {
			return StartDaemonResult{
				Started:   true,
				Message:   "timed out waiting for daemon",
				GrainPath: grainPath,
			}, fmt.Errorf("daemon did not become healthy within %s", wait)
		}
		if hErr := health(ctx); hErr == nil {
			return StartDaemonResult{
				Started:   true,
				Message:   "daemon started",
				GrainPath: grainPath,
			}, nil
		}
		sleep(poll)
	}
}

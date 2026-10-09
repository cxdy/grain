package hypervisor

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// pidIsHypervisor reports whether pid's executable matches one of fragments
// (case-insensitive substring of the base name). known is false when the
// executable path cannot be read; callers then fall back to pid liveness.
func pidIsHypervisor(pid int, fragments ...string) (match bool, known bool) {
	if pid <= 0 {
		return false, true
	}
	exe, err := processExecutable(pid)
	if err != nil || exe == "" {
		return false, false
	}
	base := strings.ToLower(filepath.Base(exe))
	for _, frag := range fragments {
		frag = strings.ToLower(strings.TrimSpace(frag))
		if frag != "" && strings.Contains(base, frag) {
			return true, true
		}
	}
	return false, true
}

// runtimeProcessRunning is the QEMU and Firecracker liveness check.
// A live PID is not enough: the kernel reuses PIDs, so a dead guest looks
// alive when an unrelated process inherits its PID. When the executable is
// not the hypervisor and a control socket was recorded, that socket must
// still accept connections. An empty socket path keeps pid-liveness so unit
// tests can stand in with the current process.
func runtimeProcessRunning(pid int, controlSock string, fragments ...string) bool {
	if pid <= 0 || !pidAlive(pid) {
		return false
	}
	match, known := pidIsHypervisor(pid, fragments...)
	if match || !known {
		return true
	}
	if controlSock == "" {
		return true
	}
	return controlSocketAlive(controlSock)
}

func controlSocketAlive(path string) bool {
	if path == "" {
		return false
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// hypervisorStillAlive reports whether any pid is still the hypervisor.
// An identifiable foreign process (PID reuse) does not count.
func hypervisorStillAlive(pids []int, fragments ...string) bool {
	for _, pid := range pids {
		match, known := pidIsHypervisor(pid, fragments...)
		if match {
			return true
		}
		if !known && pidAlive(pid) {
			return true
		}
	}
	return false
}

// signalHypervisorPIDs sends SIGTERM then SIGKILL. PIDs whose executable is
// known to be something other than the hypervisor are skipped so a reused
// PID is not killed.
func signalHypervisorPIDs(pids []int, fragments ...string) {
	for _, pid := range pids {
		if !maySignalHypervisor(pid, fragments...) {
			continue
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		_ = p.Signal(syscall.SIGTERM)
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			if !pidAlive(pid) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !maySignalHypervisor(pid, fragments...) {
			continue
		}
		_ = p.Signal(syscall.SIGKILL)
	}
}

func maySignalHypervisor(pid int, fragments ...string) bool {
	if pid <= 0 || !pidAlive(pid) {
		return false
	}
	match, known := pidIsHypervisor(pid, fragments...)
	if known && !match {
		return false
	}
	return true
}

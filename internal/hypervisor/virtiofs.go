package hypervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

// lookPathVirtiofsd finds the virtiofsd binary. Overridable in tests.
var lookPathVirtiofsd = findVirtiofsd

// commonVirtiofsdPaths are checked when virtiofsd is not on PATH (distro packages).
var commonVirtiofsdPaths = []string{
	"/usr/libexec/virtiofsd",
	"/usr/lib/virtiofsd",
	"/usr/lib/qemu/virtiofsd",
	"/usr/libexec/qemu/virtiofsd",
}

func findVirtiofsd() (string, error) {
	if p, err := exec.LookPath("virtiofsd"); err == nil {
		return p, nil
	}
	for _, p := range commonVirtiofsdPaths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("virtiofsd not found in PATH or common locations")
}

// VirtiofsdAvailable reports whether a virtiofsd binary can be executed.
func VirtiofsdAvailable() bool {
	_, err := lookPathVirtiofsd()
	return err == nil
}

func virtiofsdSocketPath(vmDir string, i int) string {
	return filepath.Join(vmDir, fmt.Sprintf("virtiofsd-%d.sock", i))
}

func virtiofsdPIDPath(vmDir string, i int) string {
	return filepath.Join(vmDir, fmt.Sprintf("virtiofsd-%d.pid", i))
}

// StartVirtiofsDaemons launches one virtiofsd per mount under vmDir.
// Socket/pid paths: virtiofsd-N.sock / virtiofsd-N.pid (N = mount index).
// On failure, any already-started daemons are stopped.
// Complete mounts are validated with ValidateMount before any daemon starts.
func StartVirtiofsDaemons(vmDir string, mounts []vm.Mount) error {
	if len(mounts) == 0 {
		return nil
	}
	for i, m := range mounts {
		if m.Host == "" || m.Tag == "" {
			continue
		}
		if err := ValidateMount(m); err != nil {
			return fmt.Errorf("mount[%d]: %w", i, err)
		}
	}
	bin, err := lookPathVirtiofsd()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		return err
	}

	var started []int
	for i, m := range mounts {
		if m.Host == "" || m.Tag == "" {
			continue
		}
		if err := startOneVirtiofsd(bin, vmDir, i, m.Host); err != nil {
			StopVirtiofsDaemons(vmDir)
			return fmt.Errorf("virtiofsd mount[%d] %s: %w", i, m.Host, err)
		}
		started = append(started, i)
	}
	_ = started
	return nil
}

func startOneVirtiofsd(bin, vmDir string, index int, sharedDir string) error {
	sock := virtiofsdSocketPath(vmDir, index)
	pidPath := virtiofsdPIDPath(vmDir, index)
	_ = os.Remove(sock)
	_ = os.Remove(pidPath)

	args := []string{
		"--socket-path=" + sock,
		"--shared-dir=" + sharedDir,
		"--cache=auto",
	}
	// Unprivileged hosts cannot create the default user-namespace sandbox.
	if os.Geteuid() != 0 {
		args = append(args, "--sandbox=none")
	}

	cmd := exec.Command(bin, args...)
	// Detach from grain's lifetime: qemu/virtiofsd are managed via pidfiles.
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	pid := cmd.Process.Pid
	// Release so reaping is not required; we track via pidfile + signals.
	_ = cmd.Process.Release()

	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		_ = killPID(pid)
		return fmt.Errorf("write pidfile: %w", err)
	}

	// Wait for the vhost-user socket to appear before QEMU connects.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := os.Stat(sock); err == nil && st.Mode()&os.ModeSocket != 0 {
			return nil
		}
		// Some FS report sockets without ModeSocket; existence is enough.
		if _, err := os.Stat(sock); err == nil {
			return nil
		}
		if !pidAlive(pid) {
			_ = os.Remove(pidPath)
			return fmt.Errorf("exited before creating socket %s", sock)
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = killPID(pid)
	_ = os.Remove(pidPath)
	_ = os.Remove(sock)
	return fmt.Errorf("timeout waiting for socket %s", sock)
}

// StopVirtiofsDaemons kills virtiofsd processes for a VM and removes sockets/pids.
func StopVirtiofsDaemons(vmDir string) {
	if vmDir == "" {
		return
	}
	entries, err := os.ReadDir(vmDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "virtiofsd-") && strings.HasSuffix(name, ".pid") {
			pidPath := filepath.Join(vmDir, name)
			if b, err := os.ReadFile(pidPath); err == nil {
				var pid int
				if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err == nil {
					_ = killPID(pid)
				}
			}
			_ = os.Remove(pidPath)
		}
		if strings.HasPrefix(name, "virtiofsd-") && strings.HasSuffix(name, ".sock") {
			_ = os.Remove(filepath.Join(vmDir, name))
		}
	}
}

func killPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = p.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = p.Signal(syscall.SIGKILL)
	return nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// virtiofsMemoryBackendArgs returns QEMU shared-memory args required by vhost-user-fs.
// size must match -m (memory MB).
func virtiofsMemoryBackendArgs(memoryMB int) []string {
	if memoryMB <= 0 {
		memoryMB = 512
	}
	return []string{
		"-object", fmt.Sprintf("memory-backend-file,id=mem,size=%dM,mem-path=/dev/shm,share=on", memoryMB),
		"-numa", "node,memdev=mem",
	}
}

// virtiofsDeviceArgs builds -chardev / -device vhost-user-fs-pci pairs.
// Socket paths must match those created by StartVirtiofsDaemons.
// Returns an error if any complete mount fails ValidateMount.
func virtiofsDeviceArgs(mounts []vm.Mount, vmDir string) ([]string, error) {
	if len(mounts) == 0 {
		return nil, nil
	}
	args := make([]string, 0, len(mounts)*4)
	for i, m := range mounts {
		if m.Host == "" || m.Tag == "" {
			continue
		}
		if err := ValidateMount(m); err != nil {
			return nil, err
		}
		charID := fmt.Sprintf("charfs%d", i)
		sock := virtiofsdSocketPath(vmDir, i)
		args = append(args,
			"-chardev", fmt.Sprintf("socket,id=%s,path=%s", charID, sock),
			"-device", fmt.Sprintf("vhost-user-fs-pci,queue-size=1024,chardev=%s,tag=%s", charID, m.Tag),
		)
	}
	return args, nil
}

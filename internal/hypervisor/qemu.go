package hypervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

// QEMURuntime launches guests with QEMU (HVF on Apple Silicon when available).
type QEMURuntime struct {
	Binary  string
	DataDir string
	// Soft mode: if qemu missing, Start returns a clear error (not panic).
}

func NewQEMURuntime(binary, dataDir string) *QEMURuntime {
	if binary == "" {
		if runtime.GOARCH == "arm64" {
			binary = "qemu-system-aarch64"
		} else {
			binary = "qemu-system-x86_64"
		}
	}
	return &QEMURuntime{Binary: binary, DataDir: dataDir}
}

func (q *QEMURuntime) Start(ctx context.Context, inst *vm.Instance, diskPath string) error {
	bin, err := exec.LookPath(q.Binary)
	if err != nil {
		return fmt.Errorf("%s not found — install qemu (brew install qemu) or use grain with mock hypervisor for tests", q.Binary)
	}
	logPath := filepath.Join(q.DataDir, "logs", inst.Name+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}

	// Host-forwarded SSH for simple access without full guest agent yet.
	sshPort := 2200 + (time.Now().Nanosecond() % 40000)
	inst.SSHPort = sshPort
	inst.IP = "127.0.0.1"

	args := []string{
		"-name", inst.Name,
		"-machine", machineType(),
		"-cpu", cpuType(),
		"-smp", strconv.Itoa(inst.CPUs),
		"-m", strconv.Itoa(inst.MemoryMB),
		"-drive", fmt.Sprintf("file=%s,if=virtio,cache=writeback", diskPath),
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", sshPort),
		"-device", "virtio-net-device,netdev=net0",
		"-nographic",
		"-serial", "file:" + logPath + ".serial",
		"-pidfile", filepath.Join(filepath.Dir(diskPath), "qemu.pid"),
		"-daemonize",
	}
	// UEFI for aarch64 when available
	if runtime.GOARCH == "arm64" {
		for _, edk := range []string{
			"/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
			"/usr/local/share/qemu/edk2-aarch64-code.fd",
			"/usr/share/AAVMF/AAVMF_CODE.fd",
		} {
			if _, err := os.Stat(edk); err == nil {
				args = append(args, "-drive", fmt.Sprintf("if=pflash,format=raw,readonly=on,file=%s", edk))
				break
			}
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("qemu: %w (see %s)", err, logPath)
	}
	_ = logFile.Close()

	// read pidfile
	pidb, err := os.ReadFile(filepath.Join(filepath.Dir(diskPath), "qemu.pid"))
	if err == nil {
		var pid int
		_, _ = fmt.Sscanf(string(pidb), "%d", &pid)
		inst.PID = pid
	}
	inst.Status = vm.StatusRunning
	return nil
}

func (q *QEMURuntime) Stop(_ context.Context, inst *vm.Instance) error {
	if inst.PID > 0 {
		proc, err := os.FindProcess(inst.PID)
		if err == nil {
			_ = proc.Signal(syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = proc.Signal(syscall.SIGKILL)
		}
	}
	inst.PID = 0
	inst.Status = vm.StatusStopped
	return nil
}

func (q *QEMURuntime) Running(inst *vm.Instance) bool {
	if inst.PID <= 0 {
		return false
	}
	proc, err := os.FindProcess(inst.PID)
	if err != nil {
		return false
	}
	// On Unix, Signal(0) checks aliveness
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func machineType() string {
	if runtime.GOARCH == "arm64" {
		if runtime.GOOS == "darwin" {
			return "virt,accel=hvf"
		}
		return "virt,accel=kvm:tcg"
	}
	if runtime.GOOS == "darwin" {
		return "q35,accel=hvf"
	}
	return "q35,accel=kvm:tcg"
}

func cpuType() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		return "host"
	}
	if runtime.GOARCH == "arm64" {
		return "host"
	}
	return "host"
}

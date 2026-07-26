package hypervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/netutil"
	"github.com/cxdy/grain/internal/vm"
)

// QEMURuntime launches guests with QEMU (HVF on Apple Silicon when available).
type QEMURuntime struct {
	Binary  string
	DataDir string
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
		return fmt.Errorf("%s not found — install qemu (brew install qemu)", q.Binary)
	}

	// Prefer existing disk.qcow2 overlay if present
	if _, err := os.Stat(diskPath + ".qcow2"); err == nil {
		diskPath = diskPath + ".qcow2"
		inst.DiskPath = diskPath
	}

	logPath := filepath.Join(q.DataDir, "logs", inst.Name+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	sshPort, err := netutil.FreeTCPPort()
	if err != nil {
		return err
	}
	inst.SSHPort = sshPort
	inst.IP = "127.0.0.1"

	pidFile := filepath.Join(filepath.Dir(diskPath), "qemu.pid")
	_ = os.Remove(pidFile)

	driveFmt := "raw"
	if strings.HasSuffix(diskPath, ".qcow2") {
		driveFmt = "qcow2"
	}

	args := []string{
		"-name", inst.Name,
		"-machine", machineType(),
		"-cpu", cpuType(),
		"-smp", strconv.Itoa(inst.CPUs),
		"-m", strconv.Itoa(inst.MemoryMB),
		"-drive", fmt.Sprintf("file=%s,if=virtio,format=%s,cache=writeback", diskPath, driveFmt),
		"-netdev", fmt.Sprintf("user,id=net0,hostfwd=tcp:127.0.0.1:%d-:22", sshPort),
		"-device", "virtio-net-device,netdev=net0",
		"-nographic",
		"-serial", "file:" + logPath + ".serial",
		"-pidfile", pidFile,
		"-daemonize",
	}

	// cloud-init seed
	seed := filepath.Join(filepath.Dir(diskPath), "seed.iso")
	if _, err := os.Stat(seed); err == nil {
		args = append(args, "-drive", fmt.Sprintf("file=%s,if=virtio,format=raw,readonly=on", seed))
	}

	// UEFI for aarch64
	if runtime.GOARCH == "arm64" {
		if edk := findEDK(); edk != "" {
			args = append(args, "-drive", fmt.Sprintf("if=pflash,format=raw,readonly=on,file=%s", edk))
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("qemu: %w (see %s)", err, logPath)
	}

	// pidfile may take a moment
	for i := 0; i < 20; i++ {
		pidb, err := os.ReadFile(pidFile)
		if err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(pidb)), "%d", &pid); err == nil && pid > 0 {
				inst.PID = pid
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	inst.Status = vm.StatusRunning
	return nil
}

func (q *QEMURuntime) Stop(_ context.Context, inst *vm.Instance) error {
	if inst.PID > 0 {
		proc, err := os.FindProcess(inst.PID)
		if err == nil {
			_ = proc.Signal(syscall.SIGTERM)
			time.Sleep(300 * time.Millisecond)
			_ = proc.Signal(syscall.SIGKILL)
		}
	}
	// also try pidfile
	if inst.DiskPath != "" {
		pidFile := filepath.Join(filepath.Dir(inst.DiskPath), "qemu.pid")
		if b, err := os.ReadFile(pidFile); err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err == nil && pid > 0 {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Signal(syscall.SIGKILL)
				}
			}
			_ = os.Remove(pidFile)
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
	return proc.Signal(syscall.Signal(0)) == nil
}

func machineType() string {
	if runtime.GOARCH == "arm64" {
		if runtime.GOOS == "darwin" {
			return "virt,accel=hvf,highmem=on"
		}
		return "virt,accel=kvm:tcg"
	}
	if runtime.GOOS == "darwin" {
		return "q35,accel=hvf"
	}
	return "q35,accel=kvm:tcg"
}

func cpuType() string {
	if runtime.GOOS == "darwin" {
		return "host"
	}
	return "host"
}

func findEDK() string {
	cands := []string{
		"/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		"/usr/local/share/qemu/edk2-aarch64-code.fd",
		"/usr/share/AAVMF/AAVMF_CODE.fd",
		"/usr/share/qemu-efi-aarch64/QEMU_EFI.fd",
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

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

	// Prefer qcow2 overlay next to requested path
	diskPath = resolveDisk(diskPath)
	inst.DiskPath = diskPath

	vmDir := filepath.Dir(diskPath)
	logPath := filepath.Join(q.DataDir, "logs", inst.Name+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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

	// Allocate any HostPort 0 entries left on the instance (manager usually does this first).
	if err := AllocateForwardPorts(inst.Forwards); err != nil {
		return err
	}

	pidFile := filepath.Join(vmDir, "qemu.pid")
	_ = os.Remove(pidFile)

	driveFmt := "raw"
	if strings.HasSuffix(diskPath, ".qcow2") {
		driveFmt = "qcow2"
	}

	// -daemonize is incompatible with -nographic; use -display none instead.
	args := []string{
		"-name", inst.Name,
		"-machine", machineType(),
		"-cpu", cpuType(),
		"-smp", strconv.Itoa(inst.CPUs),
		"-m", strconv.Itoa(inst.MemoryMB),
		"-drive", fmt.Sprintf("file=%s,if=virtio,format=%s,cache=writeback", diskPath, driveFmt),
		"-netdev", buildUserNetdev(sshPort, inst.Forwards),
		"-device", "virtio-net-pci,netdev=net0",
		"-display", "none",
		"-serial", "file:" + filepath.Join(vmDir, "serial.log"),
		"-pidfile", pidFile,
		"-daemonize",
	}

	// cloud-init NoCloud seed — attach as virtio CD so datasource is found
	seed := filepath.Join(vmDir, "seed.iso")
	if _, err := os.Stat(seed); err == nil {
		args = append(args,
			"-drive", fmt.Sprintf("if=none,id=cidata,format=raw,file=%s,readonly=on", seed),
			"-device", "virtio-blk-pci,drive=cidata,serial=cidata",
		)
	}

	// UEFI firmware for aarch64 cloud images
	if runtime.GOARCH == "arm64" {
		code, varsTemplate := findEDK()
		if code != "" {
			vars := filepath.Join(vmDir, "flash-vars.fd")
			if _, err := os.Stat(vars); err != nil {
				// copy template or create empty 64MiB vars store
				if varsTemplate != "" {
					if err := copyFile(varsTemplate, vars); err != nil {
						// fall back to empty
						_ = truncateFile(vars, 64*1024*1024)
					}
				} else {
					_ = truncateFile(vars, 64*1024*1024)
				}
			}
			args = append(args,
				"-drive", fmt.Sprintf("if=pflash,format=raw,readonly=on,file=%s", code),
				"-drive", fmt.Sprintf("if=pflash,format=raw,file=%s", vars),
			)
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		// include serial tail if any
		serial, _ := os.ReadFile(filepath.Join(vmDir, "serial.log"))
		extra := strings.TrimSpace(string(serial))
		if len(extra) > 500 {
			extra = extra[len(extra)-500:]
		}
		if extra != "" {
			return fmt.Errorf("qemu: %w (see %s)\nserial: %s", err, logPath, extra)
		}
		return fmt.Errorf("qemu: %w (see %s)", err, logPath)
	}

	for i := 0; i < 40; i++ {
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
	if inst.PID == 0 {
		return fmt.Errorf("qemu started but pidfile missing (%s)", pidFile)
	}
	inst.Status = vm.StatusRunning
	return nil
}

func resolveDisk(diskPath string) string {
	cands := []string{
		diskPath,
		diskPath + ".qcow2",
		filepath.Join(filepath.Dir(diskPath), "disk.qcow2"),
		filepath.Join(filepath.Dir(diskPath), "disk.img.qcow2"),
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p
		}
	}
	return diskPath
}

func (q *QEMURuntime) Stop(_ context.Context, inst *vm.Instance) error {
	pids := []int{}
	if inst.PID > 0 {
		pids = append(pids, inst.PID)
	}
	if inst.DiskPath != "" {
		pidFile := filepath.Join(filepath.Dir(inst.DiskPath), "qemu.pid")
		if b, err := os.ReadFile(pidFile); err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err == nil && pid > 0 {
				pids = append(pids, pid)
			}
			_ = os.Remove(pidFile)
		}
	}
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = p.Signal(syscall.SIGKILL)
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
	return "host"
}

func findEDK() (code, vars string) {
	codeCands := []string{
		"/opt/homebrew/share/qemu/edk2-aarch64-code.fd",
		"/usr/local/share/qemu/edk2-aarch64-code.fd",
		"/usr/share/AAVMF/AAVMF_CODE.fd",
		"/usr/share/qemu-efi-aarch64/QEMU_EFI.fd",
	}
	varsCands := []string{
		"/opt/homebrew/share/qemu/edk2-arm-vars.fd",
		"/usr/local/share/qemu/edk2-arm-vars.fd",
		"/usr/share/AAVMF/AAVMF_VARS.fd",
	}
	for _, p := range codeCands {
		if _, err := os.Stat(p); err == nil {
			code = p
			break
		}
	}
	for _, p := range varsCands {
		if _, err := os.Stat(p); err == nil {
			vars = p
			break
		}
	}
	return code, vars
}

func truncateFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(size)
}

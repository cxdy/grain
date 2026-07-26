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
	Binary         string
	DataDir        string
	MountDriver    string // effective driver: 9p (default) or virtiofs
	// AgentTransport is auto|tcp|vsock (see config.agent_transport).
	AgentTransport string
	// VhostVsockPath overrides the host device path for tests (default /dev/vhost-vsock).
	VhostVsockPath string
}

func NewQEMURuntime(binary, dataDir string) *QEMURuntime {
	if binary == "" {
		if runtime.GOARCH == "arm64" {
			binary = "qemu-system-aarch64"
		} else {
			binary = "qemu-system-x86_64"
		}
	}
	return &QEMURuntime{
		Binary:         binary,
		DataDir:        dataDir,
		MountDriver:    MountDriver9p,
		AgentTransport: AgentTransportAuto,
	}
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

	agentPort, err := netutil.FreeTCPPort()
	if err != nil {
		return err
	}
	inst.AgentPort = agentPort

	// Prefer virtio-vsock when host supports it; always keep TCP hostfwd as fallback.
	transport := ResolveAgentTransport(q.AgentTransport, vhostVsockAvailable(q.VhostVsockPath))
	if transport == AgentTransportVsock {
		inst.AgentCID = AllocateGuestCID(inst.Name)
	} else {
		inst.AgentCID = 0
	}

	// Allocate any HostPort 0 entries left on the instance (manager usually does this first).
	if err := AllocateForwardPorts(inst.Forwards); err != nil {
		return err
	}

	pidFile := filepath.Join(vmDir, "qemu.pid")
	_ = os.Remove(pidFile)

	qmpPath := filepath.Join(vmDir, QMPSocketName)
	_ = os.Remove(qmpPath)
	inst.QMPPath = qmpPath

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
		"-netdev", buildUserNetdev(sshPort, agentPort, inst.Forwards),
		"-device", "virtio-net-pci,netdev=net0",
		"-display", "none",
		"-serial", "file:" + filepath.Join(vmDir, "serial.log"),
		"-pidfile", pidFile,
		"-qmp", "unix:"+qmpPath+",server,nowait",
	}
	// Restore qcow2 internal snapshot taken at suspend (best-effort full memory state).
	if tag := strings.TrimSpace(inst.LoadVM); tag != "" {
		args = append(args, "-loadvm", tag)
	}
	args = append(args, "-daemonize")

	// cloud-init NoCloud seed — attach as virtio CD so datasource is found
	seed := filepath.Join(vmDir, "seed.iso")
	if _, err := os.Stat(seed); err == nil {
		args = append(args,
			"-drive", fmt.Sprintf("if=none,id=cidata,format=raw,file=%s,readonly=on", seed),
			"-device", "virtio-blk-pci,drive=cidata,serial=cidata",
		)
	}

	// Host directory mounts: 9p (default) or virtiofs (Linux + virtiofsd).
	driver := ResolveMountDriver(q.MountDriver, nil)
	if driver == MountDriverVirtioFS && len(inst.Mounts) > 0 {
		// vhost-user-fs requires a shared memory backend and a running virtiofsd
		// per share. Clean up daemons if QEMU fails to start.
		if err := StartVirtiofsDaemons(vmDir, inst.Mounts); err != nil {
			return fmt.Errorf("virtiofsd: %w", err)
		}
		args = append(args, virtiofsMemoryBackendArgs(inst.MemoryMB)...)
		args = append(args, fsdevArgs(inst.Mounts, driver, vmDir)...)
	} else {
		args = append(args, fsdevArgs(inst.Mounts, MountDriver9p, vmDir)...)
	}

	// virtio-vsock for low-latency guest agent (Linux host with /dev/vhost-vsock).
	if inst.AgentCID >= MinGuestCID {
		args = append(args, "-device", fmt.Sprintf("vhost-vsock-pci,guest-cid=%d", inst.AgentCID))
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
		if driver == MountDriverVirtioFS {
			StopVirtiofsDaemons(vmDir)
		}
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


// powerdownWait is how long to wait after QMP system_powerdown before SIGTERM/SIGKILL.
const powerdownWait = 15 * time.Second

func (q *QEMURuntime) Stop(ctx context.Context, inst *vm.Instance) error {
	pids := collectQEMUPIDs(inst)
	qmpPath := QMPPathFor(inst)
	if qmpPath != "" {
		qmpCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := qmpCommand(qmpCtx, qmpPath, "system_powerdown")
		cancel()
		if err == nil {
			deadline := time.Now().Add(powerdownWait)
			for time.Now().Before(deadline) {
				if !anyPIDAlive(pids) {
					cleanupQEMUFiles(inst)
					inst.PID = 0
					inst.QMPPath = ""
					inst.Status = vm.StatusStopped
					return nil
				}
				select {
				case <-ctx.Done():
					goto hardKill
				case <-time.After(200 * time.Millisecond):
				}
			}
		}
	}
hardKill:
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = p.Signal(syscall.SIGKILL)
		}
	}
	cleanupQEMUFiles(inst)
	inst.PID = 0
	inst.QMPPath = ""
	inst.Status = vm.StatusStopped
	return nil
}

func (q *QEMURuntime) Pause(ctx context.Context, inst *vm.Instance) error {
	if !q.Running(inst) {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	qmpPath := QMPPathFor(inst)
	if qmpPath == "" {
		return fmt.Errorf("vm %q has no QMP socket", inst.Name)
	}
	return qmpCommand(ctx, qmpPath, "stop")
}

func (q *QEMURuntime) Resume(ctx context.Context, inst *vm.Instance) error {
	if !q.Running(inst) {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	qmpPath := QMPPathFor(inst)
	if qmpPath == "" {
		return fmt.Errorf("vm %q has no QMP socket", inst.Name)
	}
	return qmpCommand(ctx, qmpPath, "cont")
}

// SaveVM creates a named qcow2 internal snapshot via HMP savevm.
// Requires a running QEMU process, QMP socket, and qcow2 root disk.
func (q *QEMURuntime) SaveVM(ctx context.Context, inst *vm.Instance, tag string) error {
	if tag == "" {
		return fmt.Errorf("snapshot tag is required")
	}
	if !q.Running(inst) {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	disk := inst.DiskPath
	if disk == "" || !strings.HasSuffix(disk, ".qcow2") {
		return fmt.Errorf("savevm requires qcow2 disk (got %q)", disk)
	}
	qmpPath := QMPPathFor(inst)
	if qmpPath == "" {
		return fmt.Errorf("vm %q has no QMP socket", inst.Name)
	}
	// savevm can take a while for large guests.
	saveCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		saveCtx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	return qmpHumanMonitor(saveCtx, qmpPath, "savevm "+tag)
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

func collectQEMUPIDs(inst *vm.Instance) []int {
	seen := map[int]struct{}{}
	var pids []int
	add := func(pid int) {
		if pid <= 0 {
			return
		}
		if _, ok := seen[pid]; ok {
			return
		}
		seen[pid] = struct{}{}
		pids = append(pids, pid)
	}
	add(inst.PID)
	if inst.DiskPath != "" {
		pidFile := filepath.Join(filepath.Dir(inst.DiskPath), "qemu.pid")
		if b, err := os.ReadFile(pidFile); err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err == nil {
				add(pid)
			}
		}
	}
	return pids
}

func anyPIDAlive(pids []int) bool {
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			if p.Signal(syscall.Signal(0)) == nil {
				return true
			}
		}
	}
	return false
}

func cleanupQEMUFiles(inst *vm.Instance) {
	if inst.DiskPath == "" {
		return
	}
	dir := filepath.Dir(inst.DiskPath)
	_ = os.Remove(filepath.Join(dir, "qemu.pid"))
	_ = os.Remove(filepath.Join(dir, QMPSocketName))
	if inst.QMPPath != "" && inst.QMPPath != filepath.Join(dir, QMPSocketName) {
		_ = os.Remove(inst.QMPPath)
	}
	// Stop any virtiofsd helpers started for this VM (no-op if none).
	StopVirtiofsDaemons(dir)
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

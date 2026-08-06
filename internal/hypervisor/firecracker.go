package hypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/hostbin"
	"github.com/cxdy/grain/internal/netutil"
	"github.com/cxdy/grain/internal/vm"
)

// Firecracker file names under the VM directory.
const (
	FCConfigName    = "firecracker.json"
	FCSocketName    = "firecracker.sock"
	FCPidName       = "firecracker.pid"
	FCVsockName     = "fc-vsock.sock"
	FCRawDiskName   = "disk.raw"
	FCDefaultBin    = "firecracker"
	FCDefaultKernel = "vmlinux"
)

// Default FC kernel boot args for a virtio root disk without PCI.
const fcDefaultBootArgs = "console=ttyS0 reboot=k panic=1 pci=off i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd root=/dev/vda rw"

// FirecrackerRuntime launches guests with the Firecracker VMM (Linux only).
// Jailer-less; agent connectivity is via Firecracker vsock (UDS on host).
// Optional single-tenant TAP + DNAT publish (vFC-2): disabled when DisableNet is set
// or when SetupFCNet fails closed with a privilege error (caller sees the error).
type FirecrackerRuntime struct {
	Binary     string // firecracker binary name or path (default: firecracker)
	DataDir    string
	KernelPath string // optional vmlinux override
	// DisableNet skips TAP/NAT (agent vsock only). Tests and degraded mode.
	DisableNet bool
}

// runtimeGOOS is the OS gate for Start (overridable in tests).
var runtimeGOOS = runtime.GOOS

// NewFirecrackerRuntime constructs a Firecracker Runtime.
// binary empty → "firecracker" (PATH lookup at Start).
// kernelPath empty → DataDir/kernels/vmlinux at Start.
func NewFirecrackerRuntime(binary, dataDir, kernelPath string) *FirecrackerRuntime {
	if binary == "" {
		binary = FCDefaultBin
	}
	return &FirecrackerRuntime{
		Binary:     binary,
		DataDir:    dataDir,
		KernelPath: kernelPath,
	}
}

// fcBootSource is the Firecracker boot-source config object.
type fcBootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args,omitempty"`
}

// fcDrive is a Firecracker drive entry.
type fcDrive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

// fcMachineConfig is the Firecracker machine-config object.
type fcMachineConfig struct {
	VCPUCount  int  `json:"vcpu_count"`
	MemSizeMiB int  `json:"mem_size_mib"`
	SMT        bool `json:"smt"`
}

// fcVsock is the Firecracker vsock device config.
type fcVsock struct {
	GuestCID int    `json:"guest_cid"`
	UDSPath  string `json:"uds_path"`
}

// fcNetworkInterface is a Firecracker network-interfaces entry (TAP host_dev_name).
type fcNetworkInterface struct {
	IfaceID     string `json:"iface_id"`
	GuestMAC    string `json:"guest_mac"`
	HostDevName string `json:"host_dev_name"`
}

// fcConfig is the full Firecracker --config-file document.
type fcConfig struct {
	BootSource        fcBootSource         `json:"boot-source"`
	Drives            []fcDrive            `json:"drives"`
	MachineConfig     fcMachineConfig      `json:"machine-config"`
	Vsock             *fcVsock             `json:"vsock,omitempty"`
	NetworkInterfaces []fcNetworkInterface `json:"network-interfaces,omitempty"`
}

// BuildFCConfig builds a Firecracker config JSON document.
// Exported for unit tests. net may be nil (vsock-only).
func BuildFCConfig(kernelPath, rootfsPath string, cpus, memMiB, guestCID int, vsockUDS string, net *FCNetPlan) fcConfig {
	if cpus < 1 {
		cpus = 1
	}
	if memMiB < 128 {
		memMiB = 128
	}
	cfg := fcConfig{
		BootSource: fcBootSource{
			KernelImagePath: kernelPath,
			BootArgs:        fcDefaultBootArgs,
		},
		Drives: []fcDrive{
			{
				DriveID:      "rootfs",
				PathOnHost:   rootfsPath,
				IsRootDevice: true,
				IsReadOnly:   false,
			},
		},
		MachineConfig: fcMachineConfig{
			VCPUCount:  cpus,
			MemSizeMiB: memMiB,
			SMT:        false,
		},
	}
	if guestCID >= MinGuestCID && vsockUDS != "" {
		cfg.Vsock = &fcVsock{
			GuestCID: guestCID,
			UDSPath:  vsockUDS,
		}
	}
	if net != nil && net.TapName != "" {
		cfg.NetworkInterfaces = []fcNetworkInterface{{
			IfaceID:     net.IfaceID,
			GuestMAC:    net.GuestMAC,
			HostDevName: net.TapName,
		}}
	}
	return cfg
}

// MarshalFCConfig returns pretty-printed Firecracker config JSON.
func MarshalFCConfig(cfg fcConfig) ([]byte, error) {
	return json.MarshalIndent(cfg, "", "  ")
}

func (f *FirecrackerRuntime) Start(ctx context.Context, inst *vm.Instance, diskPath string) error {
	if runtimeGOOS != "linux" {
		return fmt.Errorf("firecracker requires linux (current OS: %s)", runtimeGOOS)
	}

	bin, err := exec.LookPath(f.Binary)
	if err != nil {
		return fmt.Errorf("%s not found — install firecracker and ensure it is on PATH (or set firecracker_binary)", f.Binary)
	}

	kernel, err := f.resolveKernel()
	if err != nil {
		return err
	}

	diskPath = resolveDisk(diskPath)
	rawDisk, err := ensureRawRootfs(ctx, diskPath)
	if err != nil {
		return err
	}
	inst.DiskPath = rawDisk

	vmDir := filepath.Dir(rawDisk)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		return err
	}

	logPath := filepath.Join(f.DataDir, "logs", inst.Name+".log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	// Keep open for the child process; closed after Start returns via defer on failure
	// or left to OS on success after cmd inherits fd — we Close after Start succeeds.
	defer func() { _ = logFile.Close() }()

	// Agent always uses Firecracker vsock (UDS + CONNECT). Optional TAP/DNAT
	// provides hostfwd-like publish and a real guest IP (vFC-2).
	cid := AllocateGuestCID(inst.Name)
	inst.AgentCID = cid

	apiSock := filepath.Join(vmDir, FCSocketName)
	vsockUDS := filepath.Join(vmDir, FCVsockName)
	pidFile := filepath.Join(vmDir, FCPidName)
	cfgPath := filepath.Join(vmDir, FCConfigName)
	_ = os.Remove(apiSock)
	_ = os.Remove(vsockUDS)
	_ = os.Remove(pidFile)

	// Tear down any leftover net from a prior crash.
	if old, err := ReadFCNetState(vmDir); err == nil {
		_ = TeardownFCNet(old)
		RemoveFCNetState(vmDir)
	}

	var netPlan *FCNetPlan
	var netState FCNetState
	// Real TAP only on actual Linux hosts. Tests that force runtimeGOOS=linux on
	// macOS skip TAP; DisableNet skips TAP on Linux unit tests without CAP_NET_ADMIN.
	if !f.DisableNet && runtime.GOOS == "linux" {
		plan := PlanFCNet(inst.Name)
		// Allocate host ports for SSH + optional agent TCP (publish UX parity).
		sshPort, err := netutil.FreeTCPPort()
		if err != nil {
			return fmt.Errorf("allocate ssh host port: %w", err)
		}
		agentPort, err := netutil.FreeTCPPort()
		if err != nil {
			return fmt.Errorf("allocate agent host port: %w", err)
		}
		if err := AllocateForwardPorts(inst.Forwards); err != nil {
			return err
		}
		// TAP + guest addressing only. Create-time -P / live fwd use host TCP
		// proxies (manager) — OUTPUT DNAT of 127.0.0.1 never reaches the TAP.
		st, err := SetupFCNet(plan, 0, 0, nil)
		if err != nil {
			return err
		}
		netState = st
		netPlan = &plan
		inst.IP = plan.GuestIP
		// Host ports allocated for UX / create-time TCP proxies after guest eth0 is up.
		inst.SSHPort = sshPort
		inst.AgentPort = agentPort
		if err := WriteFCNetState(vmDir, netState); err != nil {
			_ = TeardownFCNet(netState)
			return fmt.Errorf("persist fc-net state: %w", err)
		}
	} else {
		inst.SSHPort = 0
		inst.AgentPort = 0
		inst.IP = ""
	}

	// Attach cloud-init seed as a secondary read-only drive when present (best-effort;
	// NoCloud typically expects an ISO/label; see guides/firecracker).
	cfg := BuildFCConfig(kernel, rawDisk, inst.CPUs, inst.MemoryMB, cid, vsockUDS, netPlan)
	seed := filepath.Join(vmDir, "seed.iso")
	if st, err := os.Stat(seed); err == nil && st.Size() > 0 {
		cfg.Drives = append(cfg.Drives, fcDrive{
			DriveID:      "cidata",
			PathOnHost:   seed,
			IsRootDevice: false,
			IsReadOnly:   true,
		})
	}

	cfgBytes, err := MarshalFCConfig(cfg)
	if err != nil {
		if netPlan != nil {
			_ = TeardownFCNet(netState)
			RemoveFCNetState(vmDir)
		}
		return fmt.Errorf("firecracker config: %w", err)
	}
	if err := os.WriteFile(cfgPath, cfgBytes, 0o644); err != nil {
		if netPlan != nil {
			_ = TeardownFCNet(netState)
			RemoveFCNetState(vmDir)
		}
		return err
	}

	// Store API socket path in QMPPath so stop/pause can locate it (reused field).
	inst.QMPPath = apiSock

	// Do not use CommandContext: create ctx ends after Start returns and would
	// kill a non-daemonized Firecracker process. Lifecycle is owned by PID/Stop.
	cmd := exec.Command(bin,
		"--api-sock", apiSock,
		"--config-file", cfgPath,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// New process group so terminal signals to the daemon do not cascade oddly.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		if netPlan != nil {
			_ = TeardownFCNet(netState)
			RemoveFCNetState(vmDir)
		}
		return fmt.Errorf("firecracker: %w (see %s)", err, logPath)
	}
	inst.PID = cmd.Process.Pid
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(inst.PID)+"\n"), 0o644)

	// Single Wait reaper — consumed on early exit, otherwise left running so we
	// do not leave zombies after Start returns success.
	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()

	// Firecracker opens the API socket *before* finishing MicroVM start.
	// KVM failures (missing /dev/kvm) exit shortly after the socket appears —
	// so we must not treat "socket exists" as success without a grace poll.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case waitErr := <-exitCh:
			if netPlan != nil {
				_ = TeardownFCNet(netState)
				RemoveFCNetState(vmDir)
			}
			return fcImmediateExitErr(logPath, waitErr)
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			select {
			case <-exitCh:
			case <-time.After(time.Second):
			}
			if netPlan != nil {
				_ = TeardownFCNet(netState)
				RemoveFCNetState(vmDir)
			}
			return ctx.Err()
		default:
		}

		if fcAPISocketReady(apiSock) {
			break
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			select {
			case waitErr := <-exitCh:
				if netPlan != nil {
					_ = TeardownFCNet(netState)
					RemoveFCNetState(vmDir)
				}
				return fcImmediateExitErr(logPath, waitErr)
			case <-time.After(300 * time.Millisecond):
				if netPlan != nil {
					_ = TeardownFCNet(netState)
					RemoveFCNetState(vmDir)
				}
				return fcImmediateExitErr(logPath, err)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Grace period after socket (or timeout): catch "open sock → die on KVM".
	graceDeadline := time.Now().Add(fcPostStartGrace)
	for time.Now().Before(graceDeadline) {
		select {
		case waitErr := <-exitCh:
			if netPlan != nil {
				_ = TeardownFCNet(netState)
				RemoveFCNetState(vmDir)
			}
			return fcImmediateExitErr(logPath, waitErr)
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			select {
			case <-exitCh:
			case <-time.After(time.Second):
			}
			if netPlan != nil {
				_ = TeardownFCNet(netState)
				RemoveFCNetState(vmDir)
			}
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
			if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
				select {
				case waitErr := <-exitCh:
					if netPlan != nil {
						_ = TeardownFCNet(netState)
						RemoveFCNetState(vmDir)
					}
					return fcImmediateExitErr(logPath, waitErr)
				case <-time.After(300 * time.Millisecond):
					if netPlan != nil {
						_ = TeardownFCNet(netState)
						RemoveFCNetState(vmDir)
					}
					return fcImmediateExitErr(logPath, err)
				}
			}
		}
	}

	if !f.Running(inst) {
		if netPlan != nil {
			_ = TeardownFCNet(netState)
			RemoveFCNetState(vmDir)
		}
		select {
		case waitErr := <-exitCh:
			return fcImmediateExitErr(logPath, waitErr)
		default:
			return fcImmediateExitErr(logPath, nil)
		}
	}

	inst.Status = vm.StatusRunning
	return nil
}

// fcPostStartGrace is how long Start waits after the API socket appears for an
// immediate Firecracker death (e.g. missing /dev/kvm). Tests may shrink it.
var fcPostStartGrace = 750 * time.Millisecond

func fcAPISocketReady(apiSock string) bool {
	if st, err := os.Stat(apiSock); err == nil && st.Mode()&os.ModeSocket != 0 {
		return true
	}
	// Some FS report sockets differently — try dial.
	if _, err := os.Stat(apiSock); err == nil {
		c, err := net.DialTimeout("unix", apiSock, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return true
		}
	}
	return false
}

// fcImmediateExitErr builds the Start error when Firecracker dies during boot.
// Includes a log tail and a KVM-specific hint when the log mentions KVM.
func fcImmediateExitErr(logPath string, waitErr error) error {
	tail := readLogTail(logPath, 800)
	hint := ""
	low := strings.ToLower(tail)
	if strings.Contains(low, "kvm") || strings.Contains(low, "/dev/kvm") {
		hint = " (KVM unavailable — Firecracker requires /dev/kvm; enable nested virtualization if this host is a VM)"
	}
	if tail != "" {
		return fmt.Errorf("firecracker exited immediately%s (see %s)\n%s", hint, logPath, tail)
	}
	if waitErr != nil {
		return fmt.Errorf("firecracker exited immediately%s: %v (see %s)", hint, waitErr, logPath)
	}
	return fmt.Errorf("firecracker exited immediately%s (see %s)", hint, logPath)
}

func readLogTail(path string, max int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if max > 0 && len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

func (f *FirecrackerRuntime) Stop(ctx context.Context, inst *vm.Instance) error {
	apiSock := fcAPISock(inst)
	pids := collectFCPIDs(inst)

	// Prefer graceful CtrlAltDel via Firecracker API (x86; may no-op on arm).
	if apiSock != "" {
		_ = fcAPIAction(ctx, apiSock, "SendCtrlAltDel")
		deadline := time.Now().Add(powerdownWait)
		for time.Now().Before(deadline) {
			if !anyPIDAlive(pids) {
				cleanupFCFiles(inst)
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

hardKill:
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
			time.Sleep(200 * time.Millisecond)
			_ = p.Signal(syscall.SIGKILL)
		}
	}
	cleanupFCFiles(inst)
	inst.PID = 0
	inst.QMPPath = ""
	inst.Status = vm.StatusStopped
	return nil
}

func (f *FirecrackerRuntime) Pause(ctx context.Context, inst *vm.Instance) error {
	if !f.Running(inst) {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	apiSock := fcAPISock(inst)
	if apiSock == "" {
		return fmt.Errorf("vm %q has no firecracker API socket", inst.Name)
	}
	return fcAPIPatchVM(ctx, apiSock, "Paused")
}

func (f *FirecrackerRuntime) Resume(ctx context.Context, inst *vm.Instance) error {
	if !f.Running(inst) {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	apiSock := fcAPISock(inst)
	if apiSock == "" {
		return fmt.Errorf("vm %q has no firecracker API socket", inst.Name)
	}
	return fcAPIPatchVM(ctx, apiSock, "Resumed")
}

// SaveVM is not supported for Firecracker (use FC snapshot API separately).
func (f *FirecrackerRuntime) SaveVM(_ context.Context, inst *vm.Instance, tag string) error {
	_ = tag
	_ = inst
	return fmt.Errorf("savevm is not supported for firecracker hypervisor")
}

func (f *FirecrackerRuntime) Running(inst *vm.Instance) bool {
	if inst.PID <= 0 {
		return false
	}
	proc, err := os.FindProcess(inst.PID)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func (f *FirecrackerRuntime) resolveKernel() (string, error) {
	cands := []string{}
	if k := strings.TrimSpace(f.KernelPath); k != "" {
		cands = append(cands, k)
	}
	if f.DataDir != "" {
		cands = append(cands,
			filepath.Join(f.DataDir, "kernels", FCDefaultKernel),
			filepath.Join(f.DataDir, "kernel", FCDefaultKernel),
			filepath.Join(f.DataDir, FCDefaultKernel),
		)
	}
	for _, p := range cands {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 && !st.IsDir() {
			return p, nil
		}
	}
	hint := "~/.grain/kernels/vmlinux"
	if f.DataDir != "" {
		hint = filepath.Join(f.DataDir, "kernels", FCDefaultKernel)
	}
	explicit := strings.TrimSpace(f.KernelPath)
	if explicit != "" {
		return "", fmt.Errorf("firecracker kernel_path %s missing or empty (BYO misconfigured) — fix kernel_path or place vmlinux at %s; import: grain image import <vmlinux> --id fc-kernel (see guides/firecracker)", explicit, hint)
	}
	return "", fmt.Errorf("firecracker kernel not found — place a vmlinux at %s or: grain image import <vmlinux> --id fc-kernel (see guides/firecracker)", hint)
}

// ensureRawRootfs returns a raw disk path suitable for Firecracker.
// qcow2 images are converted with qemu-img when available; otherwise refused.
func ensureRawRootfs(ctx context.Context, diskPath string) (string, error) {
	if diskPath == "" {
		return "", fmt.Errorf("firecracker: empty disk path")
	}
	st, err := os.Stat(diskPath)
	if err != nil {
		return "", fmt.Errorf("firecracker disk: %w", err)
	}
	if st.IsDir() {
		return "", fmt.Errorf("firecracker disk path is a directory: %s", diskPath)
	}

	// Already raw (or non-qcow2): use as-is. Firecracker accepts raw block files.
	if !strings.HasSuffix(diskPath, ".qcow2") {
		return diskPath, nil
	}

	vmDir := filepath.Dir(diskPath)
	rawPath := filepath.Join(vmDir, FCRawDiskName)

	// Reuse existing conversion if it looks current (size > 0 and mtime >= qcow2).
	if rst, err := os.Stat(rawPath); err == nil && rst.Size() > 0 && !rst.ModTime().Before(st.ModTime()) {
		return rawPath, nil
	}

	qemuImg, err := hostbin.LookPath("qemu-img")
	if err != nil {
		return "", fmt.Errorf("firecracker requires a raw rootfs (got qcow2 %s); install qemu-img to convert, or use a raw golden: qemu-img convert -O raw %s %s",
			diskPath, diskPath, rawPath)
	}

	cmd := exec.CommandContext(ctx, qemuImg, "convert", "-O", "raw", diskPath, rawPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("qemu-img convert to raw: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return rawPath, nil
}

func fcAPISock(inst *vm.Instance) string {
	if inst.QMPPath != "" {
		return inst.QMPPath
	}
	if inst.DiskPath != "" {
		p := filepath.Join(filepath.Dir(inst.DiskPath), FCSocketName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func collectFCPIDs(inst *vm.Instance) []int {
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
		pidFile := filepath.Join(filepath.Dir(inst.DiskPath), FCPidName)
		if b, err := os.ReadFile(pidFile); err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err == nil {
				add(pid)
			}
		}
	}
	return pids
}

func cleanupFCFiles(inst *vm.Instance) {
	if inst.DiskPath == "" {
		return
	}
	dir := filepath.Dir(inst.DiskPath)
	if st, err := ReadFCNetState(dir); err == nil {
		_ = TeardownFCNet(st)
		RemoveFCNetState(dir)
	}
	for _, name := range []string{FCPidName, FCSocketName, FCVsockName} {
		_ = os.Remove(filepath.Join(dir, name))
	}
	if inst.QMPPath != "" && filepath.Base(inst.QMPPath) == FCSocketName {
		_ = os.Remove(inst.QMPPath)
	}
}

// fcAPIAction sends PUT /actions with the given action_type.
func fcAPIAction(ctx context.Context, apiSock, actionType string) error {
	body := fmt.Sprintf(`{"action_type":%q}`, actionType)
	return fcAPIRequest(ctx, apiSock, http.MethodPut, "/actions", []byte(body))
}

// fcAPIPatchVM sends PATCH /vm with state Paused|Resumed.
func fcAPIPatchVM(ctx context.Context, apiSock, state string) error {
	body := fmt.Sprintf(`{"state":%q}`, state)
	return fcAPIRequest(ctx, apiSock, http.MethodPatch, "/vm", []byte(body))
}

func fcAPIRequest(ctx context.Context, apiSock, method, path string, body []byte) error {
	if apiSock == "" {
		return fmt.Errorf("empty firecracker API socket")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", apiSock)
		},
	}
	client := &http.Client{Transport: tr}

	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("firecracker API %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("firecracker API %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

package manager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/cloudinit"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/guest"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/image"
	"github.com/cxdy/grain/internal/names"
	"github.com/cxdy/grain/internal/netutil"
	"github.com/cxdy/grain/internal/sshkey"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

// agentProbeTimeout is a short health check used when the guest may already
// ship grain-agent (golden images). On failure we attempt SSH deploy.
const agentProbeTimeout = 3 * time.Second

// agentBakedWait is the initial wait when Spec.HasAgent / has_agent metadata
// says the agent is baked into the image (prefer wait over SSH deploy).
const agentBakedWait = 45 * time.Second

// agentWaitFallback is used when the ReadyTimeout budget is exhausted after SSH.
const agentWaitFallback = 60 * time.Second

// Manager orchestrates VM lifecycle: name → disk → start → record.
type Manager struct {
	cfg  config.Config
	st   *store.Store
	rt   hypervisor.Runtime
	disk hypervisor.Disk
	log  *slog.Logger
}

func New(cfg config.Config, st *store.Store, rt hypervisor.Runtime, disk hypervisor.Disk, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{cfg: cfg, st: st, rt: rt, disk: disk, log: log}
}

// CreateTimeout is the outer deadline for API create (ReadyTimeout + buffer for image/disk).
func (m *Manager) CreateTimeout() time.Duration {
	t := m.cfg.ReadyTimeout + 2*time.Minute
	if t < 5*time.Minute {
		return 5 * time.Minute
	}
	return t
}

// ReadyTimeout is the configured SSH-ready wait.
func (m *Manager) ReadyTimeout() time.Duration {
	return m.cfg.ReadyTimeout
}

func emitCreate(opts vm.CreateOpts, ev vm.CreateEvent) {
	if opts.OnEvent != nil {
		opts.OnEvent(ev)
	}
}

// Create launches a sandbox. Ephemeral by default (opts.Persistent=false).
// When opts.OnEvent is set, progress phases are emitted:
// image, disk, seed, qemu, wait_ssh, wait_agent, userdata, ready|error.
//
// WaitMode controls readiness after start:
//   - empty/"auto": agent if image HasAgent (golden), else ssh
//   - ssh: WaitSSH + soft agent deploy/wait (failure does not fail Create)
//   - agent: require guest agent health (hard fail); try agent first, SSH deploy fallback
//   - userdata: require agent, then poll until Health.UserdataRan
func (m *Manager) Create(ctx context.Context, opts vm.CreateOpts) (*vm.Instance, error) {
	existing, err := m.st.Names()
	if err != nil {
		return nil, err
	}
	name := opts.Name
	if name == "" {
		name = names.Next("sbox", existing)
	}
	if !names.Valid(name) {
		return nil, fmt.Errorf("invalid name %q", name)
	}
	if _, taken := existing[name]; taken {
		return nil, fmt.Errorf("vm %q already exists", name)
	}

	cpus := opts.CPUs
	if cpus <= 0 {
		cpus = m.cfg.DefaultCPUs
	}
	mem := opts.MemoryMB
	if mem <= 0 {
		mem = m.cfg.DefaultMemoryMB
	}
	diskGB := opts.DiskGB
	if diskGB <= 0 {
		diskGB = m.cfg.DefaultDiskGB
	}
	img := m.resolveImageID(opts.Image)

	waitMode, err := m.resolveWaitMode(opts.WaitMode, img)
	if err != nil {
		return nil, err
	}

	if err := m.checkResourceCaps(cpus, mem, ""); err != nil {
		return nil, err
	}

	fwds, err := copyAndPrepareForwards(opts.Forwards)
	if err != nil {
		return nil, err
	}
	mounts, err := prepareMounts(opts.Mounts)
	if err != nil {
		return nil, err
	}
	sockFwds, err := prepareSocketForwards(opts.SocketForwards)
	if err != nil {
		return nil, err
	}

	inst := &vm.Instance{
		Name:           name,
		Status:         vm.StatusCreating,
		Persistent:     opts.Persistent,
		CPUs:           cpus,
		MemoryMB:       mem,
		DiskGB:         diskGB,
		Image:          img,
		Tags:           opts.Tags,
		Forwards:       fwds,
		Mounts:         mounts,
		SocketForwards: sockFwds,
		CreatedAt:      time.Now().UTC(),
	}
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	emitCreate(opts, vm.CreateEvent{Phase: vm.PhaseImage, Name: name, Message: "ensuring base image"})
	base, err := m.disk.EnsureBase(ctx, img)
	if err != nil {
		return m.fail(inst, fmt.Errorf("image: %w", err), opts)
	}

	vmDir := m.st.Dir(name)
	diskPath := filepath.Join(vmDir, "disk.img")
	// qcow2 overlay may rewrite extension
	emitCreate(opts, vm.CreateEvent{Phase: vm.PhaseDisk, Name: name, Message: "cloning disk"})
	if err := m.disk.Clone(ctx, base, diskPath, diskGB); err != nil {
		return m.fail(inst, fmt.Errorf("disk: %w", err), opts)
	}
	// detect qcow2 overlay path
	if _, err := os.Stat(diskPath + ".qcow2"); err == nil {
		diskPath = diskPath + ".qcow2"
	} else if _, err := os.Stat(filepath.Join(vmDir, "disk.img.qcow2")); err == nil {
		diskPath = filepath.Join(vmDir, "disk.img.qcow2")
	}
	// also check if clone wrote disk.qcow2 directly
	if _, err := os.Stat(filepath.Join(vmDir, "disk.qcow2")); err == nil {
		diskPath = filepath.Join(vmDir, "disk.qcow2")
	}
	inst.DiskPath = diskPath

	// SSH key + cloud-init (skip for mock disks that are tiny placeholders)
	emitCreate(opts, vm.CreateEvent{Phase: vm.PhaseSeed, Name: name, Message: "writing cloud-init seed"})
	priv, pub, err := sshkey.Ensure(m.cfg.DataDir)
	if err != nil {
		return m.fail(inst, fmt.Errorf("ssh key: %w", err), opts)
	}
	// Userdata is structure-merged inside WriteNoCloud (shell → runcmd, #cloud-config → key merge).
	// Mount runcmds are injected from prepared mounts.
	// Agent-ready goldens use a minimal seed so clone boots do less cloud-init work.
	if _, err := cloudinit.WriteNoCloudOpts(vmDir, cloudinit.SeedOpts{
		Hostname: name,
		SSHPub:   pub,
		Extra:    opts.Userdata,
		Mounts:   mountSpecs(mounts),
		Minimal:  m.imageHasAgent(img),
	}); err != nil {
		// mock / missing iso tools: log and continue (SSH inject won't work)
		m.log.Warn("cloud-init seed skipped", "err", err)
	}

	emitCreate(opts, vm.CreateEvent{Phase: vm.PhaseQEMU, Name: name, Message: "starting hypervisor"})
	if err := m.rt.Start(ctx, inst, diskPath); err != nil {
		return m.fail(inst, fmt.Errorf("start: %w", err), opts)
	}
	inst.Status = vm.StatusRunning
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	readyTimeout := m.cfg.ReadyTimeout
	if opts.WaitTimeout > 0 {
		readyTimeout = opts.WaitTimeout
	}
	readyDeadline := time.Now().Add(readyTimeout)
	emit := func(ev vm.CreateEvent) { emitCreate(opts, ev) }

	if err := m.waitReady(ctx, inst, img, priv, waitMode, readyDeadline, emit); err != nil {
		return m.fail(inst, err, opts)
	}

	// Start create-time socket forwards (SSH streamlocal) once SSH is up.
	if err := m.startSocketForwards(inst); err != nil {
		m.log.Warn("socket forwards failed", "name", inst.Name, "err", err)
		// Non-fatal: VM is still usable; surface via log.
	} else if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	m.log.Info("vm created",
		"name", inst.Name,
		"persistent", inst.Persistent,
		"cpus", inst.CPUs,
		"memory_mb", inst.MemoryMB,
		"ssh_port", inst.SSHPort,
		"agent_port", inst.AgentPort,
		"wait", waitMode,
	)
	emitCreate(opts, vm.CreateEvent{
		Phase:    vm.PhaseReady,
		Name:     inst.Name,
		Message:  "ready",
		SSHPort:  inst.SSHPort,
		Instance: inst,
	})
	return inst, nil
}


// NormalizeWaitMode validates WaitMode.
// Empty or "auto" leave the mode unresolved (caller should use resolveWaitMode).
func NormalizeWaitMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		return "", nil
	case vm.WaitSSH:
		return vm.WaitSSH, nil
	case vm.WaitAgent:
		return vm.WaitAgent, nil
	case vm.WaitUserdata:
		return vm.WaitUserdata, nil
	default:
		return "", fmt.Errorf("invalid wait mode %q (want auto, ssh, agent, or userdata)", mode)
	}
}

// resolveWaitMode picks readiness: explicit mode, or agent when the image
// ships grain-agent, otherwise ssh.
func (m *Manager) resolveWaitMode(mode, img string) (string, error) {
	mode, err := NormalizeWaitMode(mode)
	if err != nil {
		return "", err
	}
	if mode != "" {
		return mode, nil
	}
	if m.imageHasAgent(img) {
		return vm.WaitAgent, nil
	}
	return vm.WaitSSH, nil
}

// resolveImageID picks the base image: explicit opt, then config, then auto.
// "auto" or empty config prefers local grain-ubuntu when Ready.
func (m *Manager) resolveImageID(opt string) string {
	img := strings.TrimSpace(opt)
	if img == "" {
		img = strings.TrimSpace(m.cfg.Image)
	}
	if img == "" || strings.EqualFold(img, "auto") {
		return image.DefaultIDFor(m.cfg.DataDir)
	}
	return img
}

// waitReady runs the post-start readiness sequence based on waitMode.
func (m *Manager) waitReady(
	ctx context.Context,
	inst *vm.Instance,
	img, priv, waitMode string,
	readyDeadline time.Time,
	emit func(vm.CreateEvent),
) error {
	isMock := m.cfg.Hypervisor == "mock"

	switch waitMode {
	case vm.WaitAgent:
		return m.waitAgentMode(ctx, inst, img, priv, readyDeadline, emit, isMock)
	case vm.WaitUserdata:
		if err := m.waitAgentMode(ctx, inst, img, priv, readyDeadline, emit, isMock); err != nil {
			return err
		}
		return m.waitUserdata(ctx, inst, readyDeadline, emit, isMock)
	default: // ssh
		return m.waitSSHMode(ctx, inst, img, priv, readyDeadline, emit, isMock)
	}
}

// waitSSHMode is the default: WaitSSH + soft agent deploy/wait.
func (m *Manager) waitSSHMode(
	ctx context.Context,
	inst *vm.Instance,
	img, priv string,
	readyDeadline time.Time,
	emit func(vm.CreateEvent),
	isMock bool,
) error {
	if emit != nil {
		emit(vm.CreateEvent{
			Phase:   vm.PhaseWaitSSH,
			Name:    inst.Name,
			Message: "waiting for ssh",
			SSHPort: inst.SSHPort,
		})
	}
	if isMock || inst.SSHPort <= 0 {
		return nil
	}
	sshUser := m.waitSSH(ctx, inst, img, priv, readyDeadline)
	if sshUser != "" && inst.AgentPort > 0 {
		_ = m.waitOrDeployAgent(ctx, inst, sshUser, priv, readyDeadline, emit, false)
	}
	return nil
}

// waitAgentMode requires agent health. Tries agent first (golden images), then
// SSH deploy fallback. Hard-fails when the agent is not ready in time.
// Mock hypervisor treats agent wait as success after start.
func (m *Manager) waitAgentMode(
	ctx context.Context,
	inst *vm.Instance,
	img, priv string,
	readyDeadline time.Time,
	emit func(vm.CreateEvent),
	isMock bool,
) error {
	if emit != nil {
		emit(vm.CreateEvent{
			Phase:   vm.PhaseWaitAgent,
			Name:    inst.Name,
			Message: "waiting for guest agent",
			SSHPort: inst.SSHPort,
		})
	}
	if isMock {
		return nil
	}
	if inst.AgentPort <= 0 {
		return fmt.Errorf("wait agent: no agent port allocated")
	}

	client := &agent.Client{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", inst.AgentPort)}

	waitFor := time.Until(readyDeadline)
	if waitFor <= 0 {
		waitFor = agentWaitFallback
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, waitFor)
	probeErr := agent.Wait(probeCtx, client)
	probeCancel()
	if probeErr == nil {
		m.log.Info("guest agent ready", "name", inst.Name, "agent_port", inst.AgentPort)
		return nil
	}

	if inst.SSHPort > 0 {
		if emit != nil {
			emit(vm.CreateEvent{
				Phase:   vm.PhaseWaitSSH,
				Name:    inst.Name,
				Message: "waiting for ssh (agent deploy)",
				SSHPort: inst.SSHPort,
			})
		}
		sshDeadline := readyDeadline
		if time.Until(sshDeadline) < agentWaitFallback {
			sshDeadline = time.Now().Add(agentWaitFallback)
		}
		sshUser := m.waitSSH(ctx, inst, img, priv, sshDeadline)
		if sshUser != "" {
			if err := m.waitOrDeployAgent(ctx, inst, sshUser, priv, sshDeadline, emit, true); err != nil {
				return fmt.Errorf("wait agent: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("wait agent: guest agent not ready: %w", probeErr)
}

// waitUserdata polls agent Health until UserdataRan is true.
func (m *Manager) waitUserdata(
	ctx context.Context,
	inst *vm.Instance,
	readyDeadline time.Time,
	emit func(vm.CreateEvent),
	isMock bool,
) error {
	if emit != nil {
		emit(vm.CreateEvent{
			Phase:   vm.PhaseUserdata,
			Name:    inst.Name,
			Message: "waiting for userdata",
			SSHPort: inst.SSHPort,
		})
	}
	if isMock {
		return nil
	}
	if inst.AgentPort <= 0 {
		return fmt.Errorf("wait userdata: no agent port allocated")
	}

	client := &agent.Client{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", inst.AgentPort)}
	waitFor := time.Until(readyDeadline)
	if waitFor <= 0 {
		waitFor = agentWaitFallback
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitFor)
	defer cancel()

	if h, err := client.Health(waitCtx); err == nil && h.UserdataRan {
		return nil
	}

	ticker := time.NewTicker(agent.WaitPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait userdata: %w", waitCtx.Err())
		case <-ticker.C:
			h, err := client.Health(waitCtx)
			if err != nil {
				continue
			}
			if h.UserdataRan {
				m.log.Info("userdata complete", "name", inst.Name)
				return nil
			}
		}
	}
}

// waitSSH blocks until SSH accepts connections. Returns the working username,
// or "" if SSH never became ready (warnings logged).
func (m *Manager) waitSSH(
	ctx context.Context,
	inst *vm.Instance,
	img, priv string,
	readyDeadline time.Time,
) string {
	user := m.resolveSSHUser(img)
	waitCtx, cancel := context.WithDeadline(ctx, readyDeadline)
	defer cancel()
	if err := guest.WaitSSH(waitCtx, inst.IP, inst.SSHPort, user, priv); err != nil {
		if err2 := guest.WaitSSH(waitCtx, inst.IP, inst.SSHPort, "grain", priv); err2 != nil {
			m.log.Warn("ssh not ready yet", "name", inst.Name, "err", err)
			return ""
		}
		return "grain"
	}
	return user
}

func (m *Manager) fail(inst *vm.Instance, err error, opts ...vm.CreateOpts) (*vm.Instance, error) {
	inst.Status = vm.StatusError
	inst.Error = err.Error()
	_ = m.st.Put(inst)
	if len(opts) > 0 {
		emitCreate(opts[0], vm.CreateEvent{
			Phase:   vm.PhaseError,
			Name:    inst.Name,
			Error:   err.Error(),
			Message: err.Error(),
		})
	}
	return nil, err
}

func (m *Manager) List() ([]*vm.Instance, error) {
	list, err := m.st.List()
	if err != nil {
		return nil, err
	}
	for _, inst := range list {
		if (inst.Status == vm.StatusRunning || inst.Status == vm.StatusPaused) && !m.rt.Running(inst) {
			m.killLiveForwards(inst)
			m.killSocketForwards(inst)
			inst.Status = vm.StatusStopped
			inst.PID = 0
			inst.QMPPath = ""
			inst.LiveForwards = nil
			_ = m.st.Put(inst)
		}
	}
	return m.st.List()
}

func (m *Manager) Get(name string) (*vm.Instance, error) {
	return m.st.Get(name)
}

func (m *Manager) Delete(ctx context.Context, name string) error {
	inst, err := m.st.Get(name)
	if err != nil {
		return err
	}
	m.killLiveForwards(inst)
	m.killSocketForwards(inst)
	if m.rt.Running(inst) {
		if err := m.rt.Stop(ctx, inst); err != nil {
			m.log.Warn("stop failed", "name", name, "err", err)
		}
	}
	if err := m.st.Delete(name); err != nil {
		return err
	}
	m.log.Info("vm deleted", "name", name, "persistent", inst.Persistent)
	return nil
}

// Shutdown stops a VM. Ephemeral VMs are deleted; persistent VMs remain stopped.
// Uses QMP system_powerdown when available, then SIGTERM/SIGKILL.
func (m *Manager) Shutdown(ctx context.Context, name string) error {
	inst, err := m.st.Get(name)
	if err != nil {
		return err
	}
	m.killLiveForwards(inst)
	m.killSocketForwards(inst)
	if err := m.rt.Stop(ctx, inst); err != nil {
		return err
	}
	if !inst.Persistent {
		return m.st.Delete(name)
	}
	inst.Status = vm.StatusStopped
	inst.PID = 0
	inst.QMPPath = ""
	inst.LiveForwards = nil
	return m.st.Put(inst)
}

// Stop is an alias for Shutdown.
func (m *Manager) Stop(ctx context.Context, name string) error {
	return m.Shutdown(ctx, name)
}


// Pause freezes guest vCPUs via QMP stop (mock tracks paused state).
func (m *Manager) Pause(ctx context.Context, name string) error {
	inst, err := m.st.Get(name)
	if err != nil {
		return err
	}
	if inst.Status == vm.StatusPaused {
		return fmt.Errorf("vm %q is already paused", name)
	}
	if inst.Status != vm.StatusRunning || !m.rt.Running(inst) {
		return fmt.Errorf("vm %q is not running", name)
	}
	if err := m.rt.Pause(ctx, inst); err != nil {
		return err
	}
	inst.Status = vm.StatusPaused
	if err := m.st.Put(inst); err != nil {
		return err
	}
	m.log.Info("vm paused", "name", name)
	return nil
}

// Resume continues a paused VM via QMP cont.
func (m *Manager) Resume(ctx context.Context, name string) error {
	inst, err := m.st.Get(name)
	if err != nil {
		return err
	}
	if inst.Status != vm.StatusPaused {
		return fmt.Errorf("vm %q is not paused (status=%s)", name, inst.Status)
	}
	if !m.rt.Running(inst) {
		return fmt.Errorf("vm %q process is not running", name)
	}
	if err := m.rt.Resume(ctx, inst); err != nil {
		return err
	}
	inst.Status = vm.StatusRunning
	if err := m.st.Put(inst); err != nil {
		return err
	}
	m.log.Info("vm resumed", "name", name)
	return nil
}

// AddForward starts an SSH local port forward (ssh -N -L) for a running VM
// and records it in inst.LiveForwards. hostPort 0 allocates a free port.
func (m *Manager) AddForward(ctx context.Context, name string, hostPort, guestPort int) (*vm.LiveForward, error) {
	inst, err := m.st.Get(name)
	if err != nil {
		return nil, err
	}
	if inst.Status != vm.StatusRunning && inst.Status != vm.StatusPaused {
		return nil, fmt.Errorf("vm %q is not running (status=%s)", name, inst.Status)
	}
	if !m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q process is not running", name)
	}
	if guestPort <= 0 || guestPort > 65535 {
		return nil, fmt.Errorf("guest port %d out of range", guestPort)
	}
	if hostPort < 0 || hostPort > 65535 {
		return nil, fmt.Errorf("host port %d out of range", hostPort)
	}
	if hostPort > 0 && hostPort < 1024 {
		return nil, fmt.Errorf("host port %d is privileged (< 1024)", hostPort)
	}
	if hostPort == 0 {
		p, err := netutil.FreeTCPPort()
		if err != nil {
			return nil, fmt.Errorf("allocate host port: %w", err)
		}
		hostPort = p
	}
	for _, f := range inst.LiveForwards {
		if f.HostPort == hostPort {
			return nil, fmt.Errorf("host port %d already forwarded on %q", hostPort, name)
		}
	}
	if inst.SSHPort <= 0 {
		return nil, fmt.Errorf("vm %q has no SSH port", name)
	}

	lf := vm.LiveForward{HostPort: hostPort, GuestPort: guestPort}

	if m.cfg.Hypervisor == "mock" {
		lf.PID = 1
		inst.LiveForwards = append(inst.LiveForwards, lf)
		if err := m.st.Put(inst); err != nil {
			return nil, err
		}
		return &lf, nil
	}

	priv, _, err := sshkey.Ensure(m.cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("ssh key: %w", err)
	}
	user := m.resolveSSHUser(inst.Image)
	host := inst.IP
	if host == "" {
		host = "127.0.0.1"
	}
	args := guest.SSHArgs(user, host, inst.SSHPort, priv)
	if len(args) == 0 {
		return nil, fmt.Errorf("ssh args empty")
	}
	userHost := args[len(args)-1]
	base := args[:len(args)-1]
	fwdSpec := fmt.Sprintf("%d:127.0.0.1:%d", hostPort, guestPort)
	full := append([]string{}, base...)
	full = append(full, "-N", "-L", fwdSpec, "-o", "ExitOnForwardFailure=yes", "-o", "BatchMode=yes", userHost)
	_ = ctx
	cmd := exec.Command("ssh", full...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh forward: %w", err)
	}
	time.Sleep(150 * time.Millisecond)
	if cmd.Process == nil {
		return nil, fmt.Errorf("ssh forward process missing")
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		_ = cmd.Wait()
		return nil, fmt.Errorf("ssh forward died: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	lf.PID = cmd.Process.Pid
	inst.LiveForwards = append(inst.LiveForwards, lf)
	if err := m.st.Put(inst); err != nil {
		_ = killPID(lf.PID)
		return nil, err
	}
	m.log.Info("live forward added", "name", name, "host_port", hostPort, "guest_port", guestPort, "pid", lf.PID)
	return &lf, nil
}

// RemoveForward stops the SSH local forward bound to hostPort on the named VM.
func (m *Manager) RemoveForward(_ context.Context, name string, hostPort int) error {
	inst, err := m.st.Get(name)
	if err != nil {
		return err
	}
	idx := -1
	for i, f := range inst.LiveForwards {
		if f.HostPort == hostPort {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no live forward on host port %d for %q", hostPort, name)
	}
	f := inst.LiveForwards[idx]
	_ = killPID(f.PID)
	inst.LiveForwards = append(inst.LiveForwards[:idx], inst.LiveForwards[idx+1:]...)
	if err := m.st.Put(inst); err != nil {
		return err
	}
	m.log.Info("live forward removed", "name", name, "host_port", hostPort)
	return nil
}

func (m *Manager) killLiveForwards(inst *vm.Instance) {
	for _, f := range inst.LiveForwards {
		_ = killPID(f.PID)
	}
	inst.LiveForwards = nil
}

// prepareSocketForwards validates and normalises create-time socket forwards.
func prepareSocketForwards(in []vm.SocketForward) ([]vm.SocketForward, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]vm.SocketForward, 0, len(in))
	seenHost := map[string]struct{}{}
	for _, f := range in {
		host := filepath.Clean(strings.TrimSpace(f.HostPath))
		guest := strings.TrimSpace(f.GuestPath)
		if host == "" || host == "." {
			return nil, fmt.Errorf("socket forward: host_path is required")
		}
		if guest == "" || !strings.HasPrefix(guest, "/") {
			return nil, fmt.Errorf("socket forward: guest_path must be absolute (got %q)", guest)
		}
		if !filepath.IsAbs(host) {
			abs, err := filepath.Abs(host)
			if err != nil {
				return nil, fmt.Errorf("socket forward host_path: %w", err)
			}
			host = abs
		}
		if _, dup := seenHost[host]; dup {
			return nil, fmt.Errorf("duplicate socket forward host_path %q", host)
		}
		seenHost[host] = struct{}{}
		// Remove stale host socket so ssh can bind.
		if st, err := os.Lstat(host); err == nil {
			if st.Mode()&os.ModeSocket != 0 {
				_ = os.Remove(host)
			} else {
				return nil, fmt.Errorf("socket forward host_path %q exists and is not a socket", host)
			}
		}
		out = append(out, vm.SocketForward{HostPath: host, GuestPath: guest})
	}
	return out, nil
}

// startSocketForwards launches ssh -N -L streamlocal forwards for each entry.
func (m *Manager) startSocketForwards(inst *vm.Instance) error {
	if len(inst.SocketForwards) == 0 {
		return nil
	}
	if m.cfg.Hypervisor == "mock" {
		for i := range inst.SocketForwards {
			inst.SocketForwards[i].PID = 1
		}
		return nil
	}
	if inst.SSHPort <= 0 {
		return fmt.Errorf("vm %q has no SSH port", inst.Name)
	}
	priv, _, err := sshkey.Ensure(m.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("ssh key: %w", err)
	}
	user := m.resolveSSHUser(inst.Image)
	host := inst.IP
	if host == "" {
		host = "127.0.0.1"
	}
	args := guest.SSHArgs(user, host, inst.SSHPort, priv)
	if len(args) == 0 {
		return fmt.Errorf("ssh args empty")
	}
	userHost := args[len(args)-1]
	base := args[:len(args)-1]

	for i := range inst.SocketForwards {
		sf := &inst.SocketForwards[i]
		// Clear any previous PID / stale host socket.
		_ = killPID(sf.PID)
		sf.PID = 0
		if st, err := os.Lstat(sf.HostPath); err == nil && st.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(sf.HostPath)
		}
		// Ensure parent dir exists for host socket.
		if err := os.MkdirAll(filepath.Dir(sf.HostPath), 0o755); err != nil {
			return fmt.Errorf("mkdir for host socket %s: %w", sf.HostPath, err)
		}
		// OpenSSH streamlocal: -L local_socket:remote_socket
		fwdSpec := sf.HostPath + ":" + sf.GuestPath
		full := append([]string{}, base...)
		full = append(full, "-N", "-L", fwdSpec, "-o", "ExitOnForwardFailure=yes", "-o", "BatchMode=yes", "-n", "-T", userHost)
		cmd := exec.Command("ssh", full...)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start socket forward %s: %w", sf.HostPath, err)
		}
		time.Sleep(150 * time.Millisecond)
		if cmd.Process == nil {
			return fmt.Errorf("socket forward process missing for %s", sf.HostPath)
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			_ = cmd.Wait()
			return fmt.Errorf("socket forward died for %s: %w", sf.HostPath, err)
		}
		go func() { _ = cmd.Wait() }()
		sf.PID = cmd.Process.Pid
		m.log.Info("socket forward started",
			"name", inst.Name,
			"host_path", sf.HostPath,
			"guest_path", sf.GuestPath,
			"pid", sf.PID,
		)
	}
	return nil
}

func (m *Manager) killSocketForwards(inst *vm.Instance) {
	for i := range inst.SocketForwards {
		_ = killPID(inst.SocketForwards[i].PID)
		inst.SocketForwards[i].PID = 0
		// Best-effort remove host socket left by ssh.
		if p := inst.SocketForwards[i].HostPath; p != "" {
			if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSocket != 0 {
				_ = os.Remove(p)
			}
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
	time.Sleep(50 * time.Millisecond)
	_ = p.Signal(syscall.SIGKILL)
	return nil
}

// Start boots a stopped persistent (or any stored) VM using its existing disk.
func (m *Manager) Start(ctx context.Context, name string) (*vm.Instance, error) {
	inst, err := m.st.Get(name)
	if err != nil {
		return nil, err
	}
	if inst.Status == vm.StatusPaused && m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q is paused; use resume", name)
	}
	if inst.Status == vm.StatusRunning && m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q already running", name)
	}
	if inst.DiskPath == "" || !DiskExists(inst.DiskPath) {
		return nil, fmt.Errorf("vm %q has no disk (disk_path missing or gone)", name)
	}

	// Starting a non-running VM increases the running count — enforce caps.
	if err := m.checkResourceCaps(inst.CPUs, inst.MemoryMB, name); err != nil {
		return nil, err
	}

	// Re-apply forwards from meta: allocate any HostPort 0, validate others.
	if err := hypervisor.ValidateForwards(inst.Forwards); err != nil {
		return nil, err
	}
	if err := hypervisor.AllocateForwardPorts(inst.Forwards); err != nil {
		return nil, err
	}
	// Re-validate mounts from meta (host dirs must still exist for QEMU 9p).
	if err := validateStoredMounts(inst.Mounts); err != nil {
		return nil, err
	}

	priv, pub, err := sshkey.Ensure(m.cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("ssh key: %w", err)
	}

	vmDir := m.st.Dir(name)
	seed := filepath.Join(vmDir, "seed.iso")
	if !DiskExists(seed) {
		if _, err := cloudinit.WriteNoCloudOpts(vmDir, cloudinit.SeedOpts{
			Hostname: name,
			SSHPub:   pub,
			Mounts:   mountSpecs(inst.Mounts),
			Minimal:  m.imageHasAgent(inst.Image),
		}); err != nil {
			m.log.Warn("cloud-init seed skipped", "err", err)
		}
	}

	// Start uses inst.Mounts for virtio-9p device args.
	if err := m.rt.Start(ctx, inst, inst.DiskPath); err != nil {
		return m.fail(inst, fmt.Errorf("start: %w", err))
	}
	inst.Status = vm.StatusRunning
	inst.Error = ""
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	readyDeadline := time.Now().Add(m.cfg.ReadyTimeout)
	if m.cfg.Hypervisor != "mock" && inst.SSHPort > 0 {
		sshUser := m.waitSSH(ctx, inst, inst.Image, priv, readyDeadline)
		if sshUser != "" && inst.AgentPort > 0 {
			_ = m.waitOrDeployAgent(ctx, inst, sshUser, priv, readyDeadline, nil, false)
		}
	}

	// Re-apply socket streamlocal forwards after SSH is up.
	if err := m.startSocketForwards(inst); err != nil {
		m.log.Warn("socket forwards failed on start", "name", inst.Name, "err", err)
	}
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	m.log.Info("vm started",
		"name", inst.Name,
		"persistent", inst.Persistent,
		"ssh_port", inst.SSHPort,
		"agent_port", inst.AgentPort,
	)
	return inst, nil
}

// resolveSSHUser picks the guest SSH username from config / image catalog.
func (m *Manager) resolveSSHUser(img string) string {
	user := m.cfg.SSHUser
	if user == "" || user == "alpine" {
		if spec, err := image.Get(img); err == nil && spec.SSHUser != "" {
			user = spec.SSHUser
		}
	}
	if user == "" {
		user = "grain"
	}
	return user
}

// waitOrDeployAgent probes the guest agent, deploys it over SSH when missing,
// then waits for /health. Failures are logged as warnings only (M1 soft dependency).
// When the image has a baked-in agent (catalog HasAgent or has_agent metadata),
// prefer a longer WaitAgent before SSH deploy (still soft-fail).
// emit may be nil (e.g. Start); when set, PhaseWaitAgent events are sent.
func (m *Manager) waitOrDeployAgent(
	ctx context.Context,
	inst *vm.Instance,
	sshUser, privKey string,
	readyDeadline time.Time,
	emit func(vm.CreateEvent),
	hard bool,
) error {
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", inst.AgentPort)
	client := &agent.Client{BaseURL: baseURL}
	baked := m.imageHasAgent(inst.Image)

	if emit != nil {
		msg := "waiting for guest agent"
		if baked {
			msg = "waiting for baked-in guest agent"
		}
		emit(vm.CreateEvent{
			Phase:   vm.PhaseWaitAgent,
			Name:    inst.Name,
			Message: msg,
			SSHPort: inst.SSHPort,
		})
	}

	// Short probe, or longer wait for golden images that already ship the agent.
	probeFor := agentProbeTimeout
	if baked {
		probeFor = agentBakedWait
		if rem := time.Until(readyDeadline); rem > 0 && rem < probeFor {
			probeFor = rem
		}
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, probeFor)
	probeErr := agent.Wait(probeCtx, client)
	probeCancel()
	if probeErr == nil {
		m.log.Info("guest agent ready", "name", inst.Name, "agent_port", inst.AgentPort, "baked", baked)
		return nil
	}

	// Deploy if we have a linux binary (golden images usually skip this path).
	binPath, err := agent.LinuxBinaryPath(m.cfg.DataDir)
	if err != nil {
		m.log.Warn("guest agent not ready (no deploy binary)",
			"name", inst.Name, "agent_port", inst.AgentPort, "err", err)
		// Still try a longer wait in case the agent comes up without our help.
	} else {
		if emit != nil {
			emit(vm.CreateEvent{
				Phase:   vm.PhaseWaitAgent,
				Name:    inst.Name,
				Message: "deploying guest agent over ssh",
				SSHPort: inst.SSHPort,
			})
		}
		m.log.Info("deploying guest agent", "name", inst.Name, "binary", binPath)
		deployCtx, deployCancel := context.WithTimeout(ctx, agentWaitFallback)
		err := guest.EnsureAgent(deployCtx, inst.IP, inst.SSHPort, sshUser, privKey, binPath)
		deployCancel()
		if err != nil {
			m.log.Warn("guest agent deploy failed", "name", inst.Name, "err", err)
		}
	}

	// Wait with remaining ReadyTimeout budget; fall back to 60s if exhausted.
	waitFor := time.Until(readyDeadline)
	if waitFor <= 0 {
		waitFor = agentWaitFallback
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, waitFor)
	defer waitCancel()
	if err := agent.Wait(waitCtx, client); err != nil {
		m.log.Warn("guest agent not ready", "name", inst.Name, "agent_port", inst.AgentPort, "err", err)
		if hard {
			return err
		}
		return nil
	}
	m.log.Info("guest agent ready", "name", inst.Name, "agent_port", inst.AgentPort)
	return nil
}

// imageHasAgent reports whether the base image ships grain-agent (catalog or local meta).
func (m *Manager) imageHasAgent(img string) bool {
	if img == "" {
		return false
	}
	return image.NewManager(m.cfg.DataDir).ImageHasAgent(img)
}

func (m *Manager) CleanupEphemeral(ctx context.Context) error {
	list, err := m.st.List()
	if err != nil {
		return err
	}
	for _, inst := range list {
		if inst.Persistent {
			continue
		}
		_ = m.Delete(ctx, inst.Name)
	}
	return nil
}

func DiskExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// copyAndPrepareForwards validates opts.Forwards, deep-copies them, and
// allocates free host ports where HostPort is 0.
func copyAndPrepareForwards(in []vm.PortForward) ([]vm.PortForward, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if err := hypervisor.ValidateForwards(in); err != nil {
		return nil, err
	}
	out := make([]vm.PortForward, len(in))
	copy(out, in)
	if err := hypervisor.AllocateForwardPorts(out); err != nil {
		return nil, err
	}
	return out, nil
}

// prepareMounts resolves host paths to absolute, rejects non-directories,
// assigns tags grain0, grain1, … when empty, and deep-copies the list.
func prepareMounts(in []vm.Mount) ([]vm.Mount, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]vm.Mount, 0, len(in))
	for i, m := range in {
		if m.Host == "" {
			return nil, fmt.Errorf("mount[%d]: empty host path", i)
		}
		if m.Guest == "" {
			return nil, fmt.Errorf("mount[%d]: empty guest path", i)
		}
		if m.Guest[0] != '/' {
			return nil, fmt.Errorf("mount[%d]: guest path %q must be absolute (start with /)", i, m.Guest)
		}
		abs, err := filepath.Abs(m.Host)
		if err != nil {
			return nil, fmt.Errorf("mount host %q: %w", m.Host, err)
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("mount host %q: %w", abs, err)
		}
		if !st.IsDir() {
			return nil, fmt.Errorf("mount host %q is not a directory", abs)
		}
		tag := m.Tag
		if tag == "" {
			tag = fmt.Sprintf("grain%d", i)
		}
		out = append(out, vm.Mount{Host: abs, Guest: m.Guest, Tag: tag})
	}
	return out, nil
}

// validateStoredMounts checks that persisted mount host paths still exist as dirs.
func validateStoredMounts(mounts []vm.Mount) error {
	for i, m := range mounts {
		if m.Host == "" || m.Tag == "" {
			return fmt.Errorf("mount[%d]: incomplete (host=%q tag=%q)", i, m.Host, m.Tag)
		}
		st, err := os.Stat(m.Host)
		if err != nil {
			return fmt.Errorf("mount host %q: %w", m.Host, err)
		}
		if !st.IsDir() {
			return fmt.Errorf("mount host %q is not a directory", m.Host)
		}
	}
	return nil
}

func mountSpecs(mounts []vm.Mount) []cloudinit.MountSpec {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]cloudinit.MountSpec, len(mounts))
	for i, m := range mounts {
		out[i] = cloudinit.MountSpec{Tag: m.Tag, Guest: m.Guest}
	}
	return out
}

// activeStatus is true for VMs that consume host resources toward caps.
// Stopped and error VMs do not count; creating does (in-flight create).
func activeStatus(s vm.Status) bool {
	return s == vm.StatusRunning || s == vm.StatusCreating || s == vm.StatusPaused
}

// checkResourceCaps rejects Create/Start when per-VM or host totals would exceed config.
// excludeName skips that instance when summing (used on Start so a stopped VM's own
// prior resources are not double-counted if it were somehow still listed as active).
// Zero config fields mean unlimited for that dimension.
func (m *Manager) checkResourceCaps(cpus, mem int, excludeName string) error {
	cfg := m.cfg
	if cfg.MaxCPUsPerVM > 0 && cpus > cfg.MaxCPUsPerVM {
		return fmt.Errorf("resource cap: max_cpus_per_vm is %d (requested %d)", cfg.MaxCPUsPerVM, cpus)
	}
	if cfg.MaxMemoryMBPerVM > 0 && mem > cfg.MaxMemoryMBPerVM {
		return fmt.Errorf("resource cap: max_memory_mb_per_vm is %d (requested %d)", cfg.MaxMemoryMBPerVM, mem)
	}

	list, err := m.st.List()
	if err != nil {
		return err
	}
	var nRunning, totalCPUs, totalMem int
	for _, inst := range list {
		if excludeName != "" && inst.Name == excludeName {
			continue
		}
		if !activeStatus(inst.Status) {
			continue
		}
		nRunning++
		totalCPUs += inst.CPUs
		totalMem += inst.MemoryMB
	}

	if cfg.MaxVMs > 0 && nRunning+1 > cfg.MaxVMs {
		return fmt.Errorf("resource cap: max_vms is %d (already %d running)", cfg.MaxVMs, nRunning)
	}
	if cfg.MaxCPUsTotal > 0 && totalCPUs+cpus > cfg.MaxCPUsTotal {
		return fmt.Errorf("resource cap: max_cpus_total is %d (already %d in use, requested %d)", cfg.MaxCPUsTotal, totalCPUs, cpus)
	}
	if cfg.MaxMemoryMBTotal > 0 && totalMem+mem > cfg.MaxMemoryMBTotal {
		return fmt.Errorf("resource cap: max_memory_mb_total is %d (already %d in use, requested %d)", cfg.MaxMemoryMBTotal, totalMem, mem)
	}
	return nil
}

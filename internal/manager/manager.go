package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	// createMu / creating claim in-flight Create names so concurrent same-name
	// creates cannot both pass the store check (TOCTOU).
	createMu sync.Mutex
	creating map[string]struct{}

	// overlayWarnOnce emits a single multi-tenant isolation warning per Manager
	// when any VM is created with network: overlay.
	overlayWarnOnce sync.Once
}

func New(cfg config.Config, st *store.Store, rt hypervisor.Runtime, disk hypervisor.Disk, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		cfg:      cfg,
		st:       st,
		rt:       rt,
		disk:     disk,
		log:      log,
		creating: make(map[string]struct{}),
	}
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

// claimCreateName reserves a VM name for an in-flight Create.
// requested empty → allocate via names.Next, treating store + in-flight as taken.
func (m *Manager) claimCreateName(requested string) (string, error) {
	m.createMu.Lock()
	defer m.createMu.Unlock()

	existing, err := m.st.Names()
	if err != nil {
		return "", err
	}
	for n := range m.creating {
		existing[n] = struct{}{}
	}

	name := requested
	if name == "" {
		name = names.Next("sbox", existing)
	}
	if !names.Valid(name) {
		return "", fmt.Errorf("invalid name %q", name)
	}
	if _, taken := existing[name]; taken {
		return "", fmt.Errorf("vm %q already exists", name)
	}
	m.creating[name] = struct{}{}
	return name, nil
}

func (m *Manager) releaseCreateName(name string) {
	m.createMu.Lock()
	delete(m.creating, name)
	m.createMu.Unlock()
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
//   - bootstrap: require agent, then poll readiness protocol until state=ready
//     (or fail on state=failed / timeout). VM is left running on bootstrap failure.
//
// Concurrent Creates for the same name are serialized via an in-flight claim so
// only one proceeds past the existence check (avoids store/disk TOCTOU races).
func (m *Manager) Create(ctx context.Context, opts vm.CreateOpts) (*vm.Instance, error) {
	name, err := m.claimCreateName(opts.Name)
	if err != nil {
		return nil, err
	}
	defer m.releaseCreateName(name)

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
	if m.cfg.Hypervisor == "firecracker" {
		for _, f := range fwds {
			proto := f.Proto
			if proto == "" {
				proto = "tcp"
			}
			if proto != "tcp" {
				return nil, fmt.Errorf("firecracker publish is TCP-only (got %s for host %d → guest %d); use QEMU for UDP hostfwd",
					proto, f.HostPort, f.GuestPort)
			}
		}
		if len(opts.Mounts) > 0 {
			return nil, fmt.Errorf("firecracker does not support host mounts (9p/virtiofs); use QEMU or omit --volume")
		}
		if len(opts.SocketForwards) > 0 {
			return nil, fmt.Errorf("firecracker does not support --publish-socket (SSH streamlocal); use QEMU")
		}
	}
	mounts, err := prepareMounts(opts.Mounts)
	if err != nil {
		return nil, err
	}
	sockFwds, err := prepareSocketForwards(opts.SocketForwards)
	if err != nil {
		return nil, err
	}

	archIn := strings.TrimSpace(opts.Arch)
	if archIn == "" {
		archIn = strings.TrimSpace(m.cfg.GuestArch)
	}
	arch, err := parseGuestArch(archIn)
	if err != nil {
		return nil, err
	}

	gpu := strings.TrimSpace(opts.GPU)
	if gpu == "" {
		gpu = strings.TrimSpace(m.cfg.GPU)
	}
	gpu = strings.ToLower(gpu)
	if gpu != "" && gpu != "virtio" {
		return nil, fmt.Errorf("unsupported gpu %q (want empty or virtio)", gpu)
	}

	network := strings.TrimSpace(opts.Network)
	if network == "" {
		network = strings.TrimSpace(m.cfg.Network)
	}
	if network == "" {
		network = "slirp"
	}
	network = strings.ToLower(network)
	if network != "slirp" && network != "overlay" {
		return nil, fmt.Errorf("unsupported network %q (want slirp or overlay)", network)
	}
	if network == "overlay" {
		// Guest agent on :7475 is unauthenticated; overlay peers share L2 and can dial it.
		m.overlayWarnOnce.Do(func() {
			m.log.Warn("network overlay: VMs share an L2 segment; guest agent on :7475 is unauthenticated — peers can control each other; use only among mutually trusted guests",
				"network", "overlay")
		})
	}

	inst := &vm.Instance{
		Name:           name,
		Status:         vm.StatusCreating,
		Persistent:     opts.Persistent,
		CPUs:           cpus,
		MemoryMB:       mem,
		DiskGB:         diskGB,
		Image:          img,
		Arch:           arch,
		GPU:            gpu,
		Network:        network,
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
	// Mount runcmds are injected from prepared mounts (9p or virtiofs).
	// Agent-ready goldens use a minimal seed so clone boots do less cloud-init work.
	mountDriver := hypervisor.ResolveMountDriver(m.cfg.MountDriver, m.log)
	if _, err := cloudinit.WriteNoCloudOpts(vmDir, cloudinit.SeedOpts{
		Hostname: name,
		SSHPub:   pub,
		Extra:    opts.Userdata,
		Mounts:   mountSpecs(mounts, mountDriver),
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
		// Ctrl+C / client cancel: do not mark a live hypervisor process as error.
		// The guest may already be agent-ready (grain sh works) even though wait aborted.
		if isContextCancel(err) && m.rt.Running(inst) {
			inst.Status = vm.StatusRunning
			inst.Error = ""
			_ = m.st.Put(inst)
			m.log.Warn("create wait canceled; vm left running", "name", inst.Name, "err", err)
			return inst, fmt.Errorf("create wait canceled (vm %q is still running — grain sh / grain ls): %w", inst.Name, err)
		}
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
		"agent_cid", inst.AgentCID,
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
	case vm.WaitBootstrap:
		return vm.WaitBootstrap, nil
	default:
		return "", fmt.Errorf("invalid wait mode %q (want auto, ssh, agent, userdata, or bootstrap)", mode)
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
	case vm.WaitBootstrap:
		if err := m.waitAgentMode(ctx, inst, img, priv, readyDeadline, emit, isMock); err != nil {
			return err
		}
		return m.waitBootstrap(ctx, inst, readyDeadline, emit, isMock)
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
	if sshUser != "" && agentTarget(inst).HasEndpoint() {
		_ = m.waitOrDeployAgent(ctx, inst, sshUser, priv, readyDeadline, emit, false)
	}
	return nil
}

// waitAgentMode requires agent health. Short-probes first (full readiness budget
// for golden images that bake the agent), then SSH-deploys grain-agent if needed.
//
// Important: do NOT spend the full ReadyTimeout on the initial probe for images
// without a baked agent — that would burn the budget before SSH deploy ever runs
// (e.g. grain act on ubuntu-cloud timed out at 25m waiting for an agent that was
// never installed). Deploy + post-deploy wait use the remaining deadline.
//
// Golden / HasAgent images use the full ReadyTimeout for the agent wait. Falling
// back to SSH after only ~45s caused grain new to sit on "waiting ssh" while the
// baked agent was still booting (or already healthy after SSH lagged).
// Mock hypervisor treats agent wait as success after start.
func (m *Manager) waitAgentMode(
	ctx context.Context,
	inst *vm.Instance,
	img, priv string,
	readyDeadline time.Time,
	emit func(vm.CreateEvent),
	isMock bool,
) error {
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
	if isMock {
		return nil
	}
	if !agentTarget(inst).HasEndpoint() {
		return fmt.Errorf("wait agent: no agent endpoint allocated")
	}

	// Non-baked: short probe, then SSH deploy. Baked: full readiness budget on agent.
	probeFor := agentProbeTimeout
	if baked {
		probeFor = time.Until(readyDeadline)
		if probeFor <= 0 {
			probeFor = agentBakedWait
		}
	}
	if rem := time.Until(readyDeadline); rem > 0 && rem < probeFor {
		probeFor = rem
	}
	// Firecracker has no TCP hostfwd fallback: agent appears only after guest
	// boot + systemd. Retry dial until budget expires (single Dial fails fast
	// with CONNECT EOF while the guest is still booting).
	if tgt := agentTarget(inst); tgt.FirecrackerUDS != "" {
		if time.Until(readyDeadline) > probeFor {
			probeFor = time.Until(readyDeadline)
		}
		if probeFor < agentBakedWait {
			probeFor = agentBakedWait
		}
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, probeFor)
	probeErr := waitAgentReachable(probeCtx, agentTarget(inst))
	probeCancel()
	if probeErr == nil {
		m.log.Info("guest agent ready", "name", inst.Name, "agent_port", inst.AgentPort, "agent_cid", inst.AgentCID)
		// vFC-2: configure guest eth0 once agent is up (vsock); TAP already on host.
		if err := m.configureFCGuestNet(ctx, inst); err != nil {
			m.log.Warn("fc guest net config failed", "name", inst.Name, "err", err)
			// Non-fatal for agent wait — publish may fail until reconfigured.
		}
		// Create-time -P: same host TCP proxy path as live grain fwd (OUTPUT DNAT
		// of 127.0.0.1 does not deliver to TAP guests).
		if err := m.startFCCreateTimeProxies(inst); err != nil {
			m.log.Warn("fc create-time publish proxies failed", "name", inst.Name, "err", err)
		}
		return nil
	}

	// Baked agent still down after full budget: soft-fail to SSH deploy only if
	// SSH comes up; keep polling the agent in parallel (it often wins).
	// Firecracker uses vsock UDS even when SSHPort is allocated for optional TCP
	// DNAT — do not wait on guest sshd (image may not run sshd).
	if inst.SSHPort > 0 && agentTarget(inst).FirecrackerUDS == "" {
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
		if m.waitSSHOrAgent(ctx, inst, img, priv, sshDeadline, emit) {
			return nil
		}
		return fmt.Errorf("wait agent: guest agent not ready and ssh never came up (initial probe: %v)", probeErr)
	}

	return wrapAgentWaitErr(inst, probeErr)
}

// configureFCGuestNet applies static eth0 addressing inside the guest over the
// agent (vsock). Host TAP is already up from FirecrackerRuntime.Start.
func (m *Manager) configureFCGuestNet(ctx context.Context, inst *vm.Instance) error {
	if m.cfg.Hypervisor != "firecracker" || inst == nil || inst.DiskPath == "" {
		return nil
	}
	vmDir := filepath.Dir(inst.DiskPath)
	st, err := hypervisor.ReadFCNetState(vmDir)
	if err != nil {
		return nil // net disabled or not set up
	}
	script := hypervisor.GuestNetConfigScript(st.FCNetPlan)
	client, err := agent.Dial(ctx, agentTarget(inst))
	if err != nil {
		return err
	}
	// Agent runs as root on FC golden/baked images.
	res, err := client.ExecBuffered(ctx, "/bin/sh", "-c", script)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("guest net script exit %d: %s%s", res.ExitCode, res.Stdout, res.Stderr)
	}
	// Ensure inst.IP matches the plan (proxy target).
	if st.GuestIP != "" {
		inst.IP = st.GuestIP
	}
	m.log.Info("fc guest net configured", "name", inst.Name, "guest_ip", st.GuestIP, "tap", st.TapName)
	return nil
}

// FCCreateTimePublishSpecs returns TCP publish mappings that still need a host
// TCP proxy (not already present in live forwards). Pure helper for tests.
//
// Includes create-time -P forwards plus optional SSH (host:22) and agent TCP
// (host:7475) when those host ports are allocated. UDP is never included —
// Firecracker publish is TCP-proxy only.
func FCCreateTimePublishSpecs(forwards []vm.PortForward, live []vm.LiveForward, sshHostPort, agentHostPort int) []vm.PortForward {
	var candidates []vm.PortForward
	for _, f := range forwards {
		if f.HostPort <= 0 || f.GuestPort <= 0 {
			continue
		}
		proto := f.Proto
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" {
			continue
		}
		candidates = append(candidates, f)
	}
	if sshHostPort > 0 {
		candidates = append(candidates, vm.PortForward{HostPort: sshHostPort, GuestPort: 22, Proto: "tcp"})
	}
	if agentHostPort > 0 {
		candidates = append(candidates, vm.PortForward{HostPort: agentHostPort, GuestPort: hypervisor.GuestAgentPort, Proto: "tcp"})
	}
	var out []vm.PortForward
	for _, f := range candidates {
		covered := false
		for _, lf := range live {
			if lf.HostPort == f.HostPort {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		// Dedupe by host port (SSH/agent vs accidental -P collision).
		dup := false
		for _, o := range out {
			if o.HostPort == f.HostPort {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		out = append(out, f)
	}
	return out
}

// startFCCreateTimeProxies starts host TCP proxies for create-time -P publishes
// and optional SSH/agent host ports after the guest has a reachable TAP IP.
// Same mechanism as grain fwd add.
func (m *Manager) startFCCreateTimeProxies(inst *vm.Instance) error {
	if m.cfg.Hypervisor != "firecracker" || inst == nil {
		return nil
	}
	if inst.IP == "" || inst.IP == "127.0.0.1" {
		return fmt.Errorf("no guest IP for create-time publish proxies")
	}
	specs := FCCreateTimePublishSpecs(inst.Forwards, inst.LiveForwards, inst.SSHPort, inst.AgentPort)
	if len(specs) == 0 {
		return nil
	}
	var first error
	for _, f := range specs {
		pid, err := startTCPProxy(f.HostPort, inst.IP, f.GuestPort)
		if err != nil {
			if first == nil {
				first = fmt.Errorf("publish %d→%s:%d: %w", f.HostPort, inst.IP, f.GuestPort, err)
			}
			m.log.Warn("fc create-time proxy failed", "host_port", f.HostPort, "guest_port", f.GuestPort, "err", err)
			continue
		}
		inst.LiveForwards = append(inst.LiveForwards, vm.LiveForward{
			HostPort:  f.HostPort,
			GuestPort: f.GuestPort,
			PID:       pid,
		})
		m.log.Info("fc create-time publish proxy", "name", inst.Name, "host_port", f.HostPort, "guest_port", f.GuestPort, "guest_ip", inst.IP, "pid", pid)
	}
	if err := m.st.Put(inst); err != nil && first == nil {
		first = err
	}
	return first
}

// wrapAgentWaitErr annotates agent wait failures. Firecracker guests only reach
// the agent via host UDS + CONNECT (no TCP hostfwd / SSH deploy fallback).
func wrapAgentWaitErr(inst *vm.Instance, probeErr error) error {
	if probeErr == nil {
		return fmt.Errorf("wait agent: guest agent not ready")
	}
	tgt := agentTarget(inst)
	if tgt.FirecrackerUDS != "" {
		return fmt.Errorf("wait agent: guest agent not ready over Firecracker vsock UDS %s: %w — run grain doctor; ensure kernel + raw rootfs ship grain-agent (no SSH deploy on FC); see guides/firecracker",
			tgt.FirecrackerUDS, probeErr)
	}
	return fmt.Errorf("wait agent: guest agent not ready: %w", probeErr)
}

// waitAgentReachable dials and polls agent health until ctx is done.
// Retries Dial (needed for Firecracker UDS CONNECT while the guest is booting).
func waitAgentReachable(ctx context.Context, tgt agent.Target) error {
	var last error
	// Immediate attempt.
	if client, err := agent.Dial(ctx, tgt); err == nil {
		if err := agent.Wait(ctx, client); err == nil {
			return nil
		} else {
			last = err
		}
	} else {
		last = err
	}

	ticker := time.NewTicker(agent.WaitPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if last != nil {
				return last
			}
			return fmt.Errorf("wait for grain-agent: %w", ctx.Err())
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				if last != nil {
					return last
				}
				return err
			}
			client, err := agent.Dial(ctx, tgt)
			if err != nil {
				last = err
				continue
			}
			if err := agent.Wait(ctx, client); err != nil {
				last = err
				continue
			}
			return nil
		}
	}
}

// waitSSHOrAgent waits until either the guest agent becomes healthy or SSH
// accepts and a hard agent deploy succeeds. Returns true when the agent is ready.
func (m *Manager) waitSSHOrAgent(
	ctx context.Context,
	inst *vm.Instance,
	img, priv string,
	deadline time.Time,
	emit func(vm.CreateEvent),
) bool {
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ready := make(chan struct{}, 2)
	go func() {
		client, err := agent.Dial(waitCtx, agentTarget(inst))
		if err != nil {
			return
		}
		if err := agent.Wait(waitCtx, client); err != nil {
			return
		}
		m.log.Info("guest agent ready (during ssh wait)", "name", inst.Name, "agent_port", inst.AgentPort)
		select {
		case ready <- struct{}{}:
		default:
		}
	}()
	go func() {
		sshUser := m.waitSSH(waitCtx, inst, img, priv, deadline)
		if sshUser == "" {
			return
		}
		if err := m.waitOrDeployAgent(ctx, inst, sshUser, priv, deadline, emit, true); err != nil {
			m.log.Warn("agent deploy after ssh failed", "name", inst.Name, "err", err)
			return
		}
		select {
		case ready <- struct{}{}:
		default:
		}
	}()

	select {
	case <-ready:
		return true
	case <-waitCtx.Done():
		return false
	}
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
	if !agentTarget(inst).HasEndpoint() {
		return fmt.Errorf("wait userdata: no agent endpoint allocated")
	}

	waitFor := time.Until(readyDeadline)
	if waitFor <= 0 {
		waitFor = agentWaitFallback
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitFor)
	defer cancel()

	client, err := agent.Dial(waitCtx, agentTarget(inst))
	if err != nil {
		return fmt.Errorf("wait userdata: dial agent: %w", err)
	}

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

// waitBootstrap polls agent readiness protocol until state=ready.
// Missing readiness/ files means pending (authors must stamp ready).
// state=failed fails create immediately; VM is left running.
func (m *Manager) waitBootstrap(
	ctx context.Context,
	inst *vm.Instance,
	readyDeadline time.Time,
	emit func(vm.CreateEvent),
	isMock bool,
) error {
	if emit != nil {
		emit(vm.CreateEvent{
			Phase:   vm.PhaseBootstrap,
			Name:    inst.Name,
			Message: "waiting for bootstrap readiness",
			SSHPort: inst.SSHPort,
		})
	}
	if isMock {
		return nil
	}
	if !agentTarget(inst).HasEndpoint() {
		return fmt.Errorf("wait bootstrap: no agent endpoint allocated")
	}

	waitFor := time.Until(readyDeadline)
	if waitFor <= 0 {
		waitFor = agentWaitFallback
	}
	waitCtx, cancel := context.WithTimeout(ctx, waitFor)
	defer cancel()

	client, err := agent.Dial(waitCtx, agentTarget(inst))
	if err != nil {
		return fmt.Errorf("wait bootstrap: dial agent: %w", err)
	}

	lastMsg := ""
	check := func(h *agent.Health) (done bool, fail error) {
		if h == nil {
			return false, nil
		}
		r := h.Readiness
		if r == nil || strings.TrimSpace(r.State) == "" {
			// No protocol files yet → pending.
			msg := "waiting for readiness (pending)"
			if emit != nil && msg != lastMsg {
				lastMsg = msg
				emit(vm.CreateEvent{
					Phase:   vm.PhaseBootstrap,
					Name:    inst.Name,
					Message: msg,
					SSHPort: inst.SSHPort,
				})
			}
			return false, nil
		}
		msg := r.StatusLine()
		if msg == "" {
			msg = "bootstrap " + r.State
		}
		if emit != nil && msg != lastMsg {
			lastMsg = msg
			emit(vm.CreateEvent{
				Phase:   vm.PhaseBootstrap,
				Name:    inst.Name,
				Message: msg,
				SSHPort: inst.SSHPort,
			})
		}
		switch strings.ToLower(strings.TrimSpace(r.State)) {
		case agent.ReadinessReady:
			m.log.Info("bootstrap ready", "name", inst.Name, "ready_name", r.ReadyName)
			return true, nil
		case agent.ReadinessFailed:
			errMsg := r.Error
			if errMsg == "" {
				errMsg = r.Message
			}
			if errMsg == "" {
				errMsg = "bootstrap failed"
			}
			return false, fmt.Errorf("wait bootstrap: %s", errMsg)
		default:
			// pending, running, or unknown — keep polling
			return false, nil
		}
	}

	if h, err := client.Health(waitCtx); err == nil {
		done, ferr := check(h)
		if ferr != nil {
			return ferr
		}
		if done {
			return nil
		}
	}

	ticker := time.NewTicker(agent.WaitPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait bootstrap: %w", waitCtx.Err())
		case <-ticker.C:
			h, err := client.Health(waitCtx)
			if err != nil {
				continue
			}
			done, ferr := check(h)
			if ferr != nil {
				return ferr
			}
			if done {
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

func isContextCancel(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	// agent.Wait / guest.WaitSSH may wrap cancel without always preserving errors.Is.
	msg := err.Error()
	return strings.Contains(msg, "context canceled") || strings.Contains(msg, "context cancelled")
}

func (m *Manager) List() ([]*vm.Instance, error) {
	list, err := m.st.List()
	if err != nil {
		return nil, err
	}
	for _, inst := range list {
		running := m.rt.Running(inst)
		if (inst.Status == vm.StatusRunning || inst.Status == vm.StatusPaused) && !running {
			m.killLiveForwards(inst)
			m.killSocketForwards(inst)
			inst.Status = vm.StatusStopped
			inst.PID = 0
			inst.QMPPath = ""
			inst.LiveForwards = nil
			_ = m.st.Put(inst)
			continue
		}
		// Reconcile: wait aborted (Ctrl+C) used to leave StatusError while QEMU lives.
		if running && (inst.Status == vm.StatusError || inst.Status == vm.StatusCreating) {
			inst.Status = vm.StatusRunning
			inst.Error = ""
			_ = m.st.Put(inst)
		}
	}
	return m.st.List()
}

func (m *Manager) Get(name string) (*vm.Instance, error) {
	return m.st.Get(name)
}

// Clone creates a stopped persistent VM by copying the source root disk and
// adapting metadata. Source must be a persistent VM that is not running
// (status stopped, suspended, or error with disk retained). Running, paused,
// creating, and ephemeral VMs are refused — stop first.
//
// Destination name may be empty (auto sbox-N). Host SSH/agent ports and
// SLIRP hostfwd host ports are cleared so the next Start allocates fresh ports.
// Live SSH forwards are not copied. Cloud-init seed is regenerated for the new
// hostname (guest OS identity may still match the source until reconfigured).
//
// Disk copy prefers APFS clonefile / file copy so qcow2 overlays keep their
// backing chain (small and fast).
func (m *Manager) Clone(ctx context.Context, srcName, dstName string) (*vm.Instance, error) {
	srcName = strings.TrimSpace(srcName)
	if srcName == "" {
		return nil, fmt.Errorf("source name is required")
	}
	src, err := m.st.Get(srcName)
	if err != nil {
		return nil, err
	}
	if !src.Persistent {
		return nil, fmt.Errorf("cannot clone ephemeral VM %q; only persistent VMs can be cloned (stop and recreate with -p, or clone a persistent lab)", srcName)
	}
	if m.rt.Running(src) || src.Status == vm.StatusRunning || src.Status == vm.StatusPaused {
		return nil, fmt.Errorf("cannot clone running or paused VM %q; stop it first (grain stop %s)", srcName, srcName)
	}
	if src.Status == vm.StatusCreating {
		return nil, fmt.Errorf("cannot clone VM %q while status is %s", srcName, src.Status)
	}
	if src.DiskPath == "" || !DiskExists(src.DiskPath) {
		return nil, fmt.Errorf("vm %q has no disk to clone", srcName)
	}

	name, err := m.claimCreateName(strings.TrimSpace(dstName))
	if err != nil {
		return nil, err
	}
	defer m.releaseCreateName(name)

	dstDir := m.st.Dir(name)
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return nil, err
	}
	// Best-effort cleanup if we fail after claiming the directory.
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dstDir)
		}
	}()

	baseName := filepath.Base(src.DiskPath)
	if baseName == "" || baseName == "." || baseName == string(filepath.Separator) {
		baseName = "disk.qcow2"
	}
	dstDisk := filepath.Join(dstDir, baseName)
	if err := hypervisor.CopyDiskFile(ctx, src.DiskPath, dstDisk, false); err != nil {
		return nil, fmt.Errorf("copy disk: %w", err)
	}

	// Optional UEFI NVRAM — copy when present so boot order survives.
	srcVars := filepath.Join(m.st.Dir(srcName), "flash-vars.fd")
	if DiskExists(srcVars) {
		dstVars := filepath.Join(dstDir, "flash-vars.fd")
		if err := hypervisor.CopyDiskFile(ctx, srcVars, dstVars, false); err != nil {
			m.log.Warn("clone: flash-vars copy skipped", "err", err)
		}
	}

	fwds := clonePortForwards(src.Forwards)
	socks := cloneSocketForwards(src.SocketForwards)
	mounts := cloneMounts(src.Mounts)
	tags := cloneTags(src.Tags)

	inst := &vm.Instance{
		Name:           name,
		Status:         vm.StatusStopped,
		Persistent:     true,
		CPUs:           src.CPUs,
		MemoryMB:       src.MemoryMB,
		DiskGB:         src.DiskGB,
		Image:          src.Image,
		Arch:           src.Arch,
		GPU:            src.GPU,
		Network:        src.Network,
		Tags:           tags,
		Forwards:       fwds,
		Mounts:         mounts,
		SocketForwards: socks,
		DiskPath:       dstDisk,
		CreatedAt:      time.Now().UTC(),
		// Ports cleared — Start allocates SSH/agent and hostfwd HostPort 0.
		SSHPort:   0,
		AgentPort: 0,
		AgentCID:  0,
		IP:        "",
		PID:       0,
		QMPPath:   "",
		Error:     "",
	}

	// Fresh cloud-init seed for the new hostname (first-boot / missing seed path).
	// Already-provisioned guest disks keep their internal hostname until the user changes it.
	if _, pub, err := sshkey.Ensure(m.cfg.DataDir); err == nil {
		mountDriver := hypervisor.ResolveMountDriver(m.cfg.MountDriver, m.log)
		if _, err := cloudinit.WriteNoCloudOpts(dstDir, cloudinit.SeedOpts{
			Hostname: name,
			SSHPub:   pub,
			Mounts:   mountSpecs(mounts, mountDriver),
			Minimal:  m.imageHasAgent(src.Image),
		}); err != nil {
			m.log.Warn("clone: cloud-init seed skipped", "err", err)
		}
	}

	if err := m.st.Put(inst); err != nil {
		return nil, err
	}
	success = true
	m.log.Info("vm cloned", "src", srcName, "dst", name, "disk", dstDisk)
	return inst, nil
}

func clonePortForwards(in []vm.PortForward) []vm.PortForward {
	if len(in) == 0 {
		return nil
	}
	out := make([]vm.PortForward, len(in))
	for i, f := range in {
		// HostPort 0 → free port allocated on next Start (avoids conflicts with source).
		out[i] = vm.PortForward{
			HostPort:  0,
			GuestPort: f.GuestPort,
			Proto:     f.Proto,
		}
	}
	return out
}

func cloneSocketForwards(in []vm.SocketForward) []vm.SocketForward {
	if len(in) == 0 {
		return nil
	}
	out := make([]vm.SocketForward, len(in))
	for i, s := range in {
		out[i] = vm.SocketForward{
			HostPath:  s.HostPath,
			GuestPath: s.GuestPath,
			// PID cleared — re-applied on Start when configured.
		}
	}
	return out
}

func cloneMounts(in []vm.Mount) []vm.Mount {
	if len(in) == 0 {
		return nil
	}
	out := make([]vm.Mount, len(in))
	copy(out, in)
	return out
}

func cloneTags(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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

// Suspend stops a persistent VM and frees host RAM. Differs from Pause (which
// freezes vCPUs while keeping the QEMU process alive).
//
// When the root disk is qcow2, best-effort HMP savevm stores a memory+device
// snapshot under tag grain-suspend; restore then passes -loadvm. On savevm
// failure, suspend still succeeds as a disk-persist stop (cold boot on restore).
func (m *Manager) Suspend(ctx context.Context, name string) error {
	inst, err := m.st.Get(name)
	if err != nil {
		return err
	}
	if !inst.Persistent {
		return fmt.Errorf("vm %q is ephemeral; suspend requires a persistent VM (use stop/rm)", name)
	}
	if inst.Status == vm.StatusSuspended {
		return fmt.Errorf("vm %q is already suspended", name)
	}
	if inst.Status != vm.StatusRunning && inst.Status != vm.StatusPaused {
		return fmt.Errorf("vm %q is not running (status=%s)", name, inst.Status)
	}
	if !m.rt.Running(inst) {
		return fmt.Errorf("vm %q process is not running", name)
	}

	vmDir := m.st.Dir(name)
	savedSnap := false
	if strings.HasSuffix(inst.DiskPath, ".qcow2") || diskLooksQcow2(inst.DiskPath) {
		if err := m.rt.SaveVM(ctx, inst, hypervisor.SuspendSnapshotTag); err != nil {
			m.log.Warn("savevm failed; suspending with disk state only",
				"name", name, "err", err)
			clearSuspendMarker(vmDir)
		} else {
			if err := writeSuspendMarker(vmDir, hypervisor.SuspendSnapshotTag); err != nil {
				m.log.Warn("suspend marker write failed", "name", name, "err", err)
			} else {
				savedSnap = true
			}
		}
	} else {
		// Mock and raw disks: still mark for restore path consistency when SaveVM works.
		if err := m.rt.SaveVM(ctx, inst, hypervisor.SuspendSnapshotTag); err == nil {
			if err := writeSuspendMarker(vmDir, hypervisor.SuspendSnapshotTag); err == nil {
				savedSnap = true
			}
		} else {
			clearSuspendMarker(vmDir)
		}
	}

	m.killLiveForwards(inst)
	m.killSocketForwards(inst)
	if err := m.rt.Stop(ctx, inst); err != nil {
		return err
	}
	inst.Status = vm.StatusSuspended
	inst.SuspendedAt = time.Now().UTC()
	inst.PID = 0
	inst.QMPPath = ""
	inst.LiveForwards = nil
	inst.LoadVM = ""
	if err := m.st.Put(inst); err != nil {
		return err
	}
	m.log.Info("vm suspended", "name", name, "snapshot", savedSnap)
	return nil
}

// Restore boots a suspended VM. If a savevm snapshot marker exists, QEMU is
// started with -loadvm; otherwise this is a cold start from the preserved disk
// (same as Start, but only allowed from status=suspended).
func (m *Manager) Restore(ctx context.Context, name string) (*vm.Instance, error) {
	inst, err := m.st.Get(name)
	if err != nil {
		return nil, err
	}
	if inst.Status != vm.StatusSuspended {
		return nil, fmt.Errorf("vm %q is not suspended (status=%s); use start for stopped VMs", name, inst.Status)
	}
	if m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q process is unexpectedly still running", name)
	}
	if tag, ok := readSuspendMarker(m.st.Dir(name)); ok {
		inst.LoadVM = tag
	}
	return m.startFromDisk(ctx, inst)
}

func writeSuspendMarker(vmDir, tag string) error {
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(vmDir, hypervisor.SuspendMarkerName), []byte(tag+"\n"), 0o644)
}

func readSuspendMarker(vmDir string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(vmDir, hypervisor.SuspendMarkerName))
	if err != nil {
		return "", false
	}
	tag := strings.TrimSpace(string(b))
	if tag == "" {
		return "", false
	}
	return tag, true
}

func clearSuspendMarker(vmDir string) {
	_ = os.Remove(filepath.Join(vmDir, hypervisor.SuspendMarkerName))
}

func diskLooksQcow2(path string) bool {
	if path == "" {
		return false
	}
	// resolveDisk may have already rewritten to .qcow2; also check common siblings.
	if strings.HasSuffix(path, ".qcow2") {
		return true
	}
	for _, p := range []string{path + ".qcow2", filepath.Join(filepath.Dir(path), "disk.qcow2")} {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return true
		}
	}
	return false
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
	lf := vm.LiveForward{HostPort: hostPort, GuestPort: guestPort}

	if m.cfg.Hypervisor == "mock" {
		lf.PID = 1
		inst.LiveForwards = append(inst.LiveForwards, lf)
		if err := m.st.Put(inst); err != nil {
			return nil, err
		}
		return &lf, nil
	}

	// Firecracker (and any guest with a real IP but no SSH hostfwd): host TCP
	// proxy 127.0.0.1:host → guestIP:guest. QEMU still uses SSH -L when SSHPort>0.
	if m.cfg.Hypervisor == "firecracker" || (inst.SSHPort <= 0 && inst.IP != "" && inst.IP != "127.0.0.1") {
		if inst.IP == "" || inst.IP == "127.0.0.1" {
			return nil, fmt.Errorf("vm %q has no guest IP for live forward (Firecracker TAP net required)", name)
		}
		pid, err := startTCPProxy(hostPort, inst.IP, guestPort)
		if err != nil {
			return nil, err
		}
		lf.PID = pid
		inst.LiveForwards = append(inst.LiveForwards, lf)
		if err := m.st.Put(inst); err != nil {
			_ = killPID(lf.PID)
			return nil, err
		}
		m.log.Info("live forward added (tcp proxy)", "name", name, "host_port", hostPort, "guest_port", guestPort, "guest_ip", inst.IP, "pid", lf.PID)
		return &lf, nil
	}

	if inst.SSHPort <= 0 {
		return nil, fmt.Errorf("vm %q has no SSH port", name)
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
// Suspended VMs must use Restore (may load a savevm snapshot).
func (m *Manager) Start(ctx context.Context, name string) (*vm.Instance, error) {
	inst, err := m.st.Get(name)
	if err != nil {
		return nil, err
	}
	if inst.Status == vm.StatusSuspended {
		return nil, fmt.Errorf("vm %q is suspended; use restore", name)
	}
	if inst.Status == vm.StatusPaused && m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q is paused; use resume", name)
	}
	if inst.Status == vm.StatusRunning && m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q already running", name)
	}
	// Cold start: never load a suspend snapshot.
	inst.LoadVM = ""
	return m.startFromDisk(ctx, inst)
}

// startFromDisk boots a VM from its existing disk (and optional inst.LoadVM tag).
func (m *Manager) startFromDisk(ctx context.Context, inst *vm.Instance) (*vm.Instance, error) {
	name := inst.Name
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
	// Re-validate mounts from meta (host dirs must still exist for QEMU shares).
	if err := validateStoredMounts(inst.Mounts); err != nil {
		return nil, err
	}

	priv, pub, err := sshkey.Ensure(m.cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("ssh key: %w", err)
	}

	vmDir := m.st.Dir(name)
	seed := filepath.Join(vmDir, "seed.iso")
	mountDriver := hypervisor.ResolveMountDriver(m.cfg.MountDriver, m.log)
	if !DiskExists(seed) {
		if _, err := cloudinit.WriteNoCloudOpts(vmDir, cloudinit.SeedOpts{
			Hostname: name,
			SSHPub:   pub,
			Mounts:   mountSpecs(inst.Mounts, mountDriver),
			Minimal:  m.imageHasAgent(inst.Image),
		}); err != nil {
			m.log.Warn("cloud-init seed skipped", "err", err)
		}
	}

	loadTag := inst.LoadVM
	// Start uses inst.Mounts for shared-fs device args (9p or virtiofs).
	if err := m.rt.Start(ctx, inst, inst.DiskPath); err != nil {
		inst.LoadVM = ""
		return m.fail(inst, fmt.Errorf("start: %w", err))
	}
	inst.LoadVM = ""
	inst.Status = vm.StatusRunning
	inst.Error = ""
	inst.SuspendedAt = time.Time{}
	// Snapshot was consumed (or not present); clear marker so a later cold start
	// does not accidentally -loadvm.
	if loadTag != "" {
		clearSuspendMarker(vmDir)
	}
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	readyDeadline := time.Now().Add(m.cfg.ReadyTimeout)
	if m.cfg.Hypervisor != "mock" && inst.SSHPort > 0 {
		sshUser := m.waitSSH(ctx, inst, inst.Image, priv, readyDeadline)
		if sshUser != "" && agentTarget(inst).HasEndpoint() {
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
		"agent_cid", inst.AgentCID,
		"loadvm", loadTag != "",
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
	if tgt := agentTarget(inst); tgt.FirecrackerUDS != "" {
		if rem := time.Until(readyDeadline); rem > probeFor {
			probeFor = rem
		}
		if probeFor < agentBakedWait {
			probeFor = agentBakedWait
		}
	}
	probeCtx, probeCancel := context.WithTimeout(ctx, probeFor)
	probeErr := waitAgentReachable(probeCtx, agentTarget(inst))
	probeCancel()
	if probeErr == nil {
		m.log.Info("guest agent ready", "name", inst.Name, "agent_port", inst.AgentPort, "agent_cid", inst.AgentCID, "baked", baked)
		return nil
	}

	// Deploy over SSH when hostfwd is available (not Firecracker vsock-only).
	if inst.SSHPort > 0 {
		binPath, err := agent.LinuxBinaryPath(m.cfg.DataDir)
		if err != nil {
			m.log.Warn("guest agent not ready (no deploy binary)",
				"name", inst.Name, "agent_port", inst.AgentPort, "agent_cid", inst.AgentCID, "err", err)
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
	}

	// Wait with remaining ReadyTimeout budget; fall back to 60s if exhausted.
	waitFor := time.Until(readyDeadline)
	if waitFor <= 0 {
		waitFor = agentWaitFallback
	}
	waitCtx, waitCancel := context.WithTimeout(ctx, waitFor)
	defer waitCancel()
	if err := waitAgentReachable(waitCtx, agentTarget(inst)); err != nil {
		m.log.Warn("guest agent not ready", "name", inst.Name, "agent_port", inst.AgentPort, "agent_cid", inst.AgentCID, "err", err)
		if hard {
			return wrapAgentWaitErr(inst, err)
		}
		return nil
	}
	m.log.Info("guest agent ready", "name", inst.Name, "agent_port", inst.AgentPort, "agent_cid", inst.AgentCID)
	return nil
}

// AgentDeployResult is returned after a successful host→guest agent deploy.
type AgentDeployResult struct {
	Name   string        `json:"name"`
	Binary string        `json:"binary"`
	Health *agent.Health `json:"health,omitempty"`
}

// DeployAgent SCPs the daemon host's grain-agent Linux binary into the guest,
// installs the systemd unit, and restarts the service. The binary must exist
// on the daemon host (see agent.LinuxBinaryPath / just agent-linux).
// SSH hostfwd is used, so this must run on the machine that owns the VM.
func (m *Manager) DeployAgent(ctx context.Context, name string) (*AgentDeployResult, error) {
	inst, err := m.st.Get(name)
	if err != nil {
		return nil, err
	}
	if inst.Status != vm.StatusRunning && !m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q is not running", name)
	}
	if inst.SSHPort <= 0 {
		return nil, fmt.Errorf("vm %q has no SSH port", name)
	}

	binPath, err := agent.LinuxBinaryPath(m.cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("agent binary not found on daemon host (run: just agent-linux): %w", err)
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

	m.log.Info("deploying guest agent", "name", name, "binary", binPath, "ssh", fmt.Sprintf("%s@%s:%d", user, host, inst.SSHPort))
	if err := guest.EnsureAgent(ctx, host, inst.SSHPort, user, priv, binPath); err != nil {
		return nil, err
	}

	result := &AgentDeployResult{Name: name, Binary: binPath}
	if !agentTarget(inst).HasEndpoint() {
		return result, nil
	}
	// Best-effort health probe so callers can report the new agent version.
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := agent.Dial(waitCtx, agentTarget(inst))
	if err != nil {
		return result, nil
	}
	if err := agent.Wait(waitCtx, client); err != nil {
		return result, nil
	}
	h, err := client.Health(waitCtx)
	if err != nil {
		return result, nil
	}
	result.Health = h
	return result, nil
}

// agentTarget builds an agent.Dial target from instance metadata.
// Firecracker guests use host UDS + CONNECT (see agent.TargetForInstance).
func agentTarget(inst *vm.Instance) agent.Target {
	if inst == nil {
		return agent.Target{}
	}
	return agent.TargetForInstance(inst.AgentCID, inst.AgentPort, inst.DiskPath)
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
// assigns tags grain0, grain1, … when empty, validates host/tag for QEMU
// option-string safety, and deep-copies the list.
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
		mount := vm.Mount{Host: abs, Guest: m.Guest, Tag: tag}
		// Reject commas/controls in path and unsafe tags before they reach QEMU.
		if err := hypervisor.ValidateMount(mount); err != nil {
			return nil, fmt.Errorf("mount[%d]: %w", i, err)
		}
		out = append(out, mount)
	}
	return out, nil
}

// validateStoredMounts checks that persisted mount host paths still exist as dirs
// and remain safe for QEMU option strings (path/tag sanitization).
func validateStoredMounts(mounts []vm.Mount) error {
	for i, m := range mounts {
		if m.Host == "" || m.Tag == "" {
			return fmt.Errorf("mount[%d]: incomplete (host=%q tag=%q)", i, m.Host, m.Tag)
		}
		if err := hypervisor.ValidateMount(m); err != nil {
			return fmt.Errorf("mount[%d]: %w", i, err)
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

func mountSpecs(mounts []vm.Mount, driver string) []cloudinit.MountSpec {
	if len(mounts) == 0 {
		return nil
	}
	if driver == "" {
		driver = "9p"
	}
	out := make([]cloudinit.MountSpec, len(mounts))
	for i, m := range mounts {
		out[i] = cloudinit.MountSpec{Tag: m.Tag, Guest: m.Guest, Driver: driver}
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

// parseGuestArch maps user arch strings to arm64|amd64|"" (host default).
func parseGuestArch(a string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "", "host", "native", "auto":
		return "", nil
	case "arm64", "aarch64":
		return "arm64", nil
	case "amd64", "x86_64", "x86-64", "x64":
		return "amd64", nil
	default:
		return "", fmt.Errorf("unsupported arch %q (want arm64, amd64, or empty for host)", a)
	}
}

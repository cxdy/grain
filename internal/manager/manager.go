package manager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cxdy/grain/internal/cloudinit"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/guest"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/image"
	"github.com/cxdy/grain/internal/names"
	"github.com/cxdy/grain/internal/sshkey"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

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

// Create launches a sandbox. Ephemeral by default (opts.Persistent=false).
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
	img := opts.Image
	if img == "" {
		img = m.cfg.Image
	}
	if img == "" {
		img = image.DefaultID()
	}

	inst := &vm.Instance{
		Name:       name,
		Status:     vm.StatusCreating,
		Persistent: opts.Persistent,
		CPUs:       cpus,
		MemoryMB:   mem,
		DiskGB:     diskGB,
		Image:      img,
		Tags:       opts.Tags,
		CreatedAt:  time.Now().UTC(),
	}
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	base, err := m.disk.EnsureBase(ctx, img)
	if err != nil {
		return m.fail(inst, fmt.Errorf("image: %w", err))
	}

	vmDir := m.st.Dir(name)
	diskPath := filepath.Join(vmDir, "disk.img")
	// qcow2 overlay may rewrite extension
	if err := m.disk.Clone(ctx, base, diskPath, diskGB); err != nil {
		return m.fail(inst, fmt.Errorf("disk: %w", err))
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
	priv, pub, err := sshkey.Ensure(m.cfg.DataDir)
	if err != nil {
		return m.fail(inst, fmt.Errorf("ssh key: %w", err))
	}
	_ = priv
	// Userdata is structure-merged inside WriteNoCloud (shell → runcmd, #cloud-config → key merge).
	if _, err := cloudinit.WriteNoCloud(vmDir, name, pub, opts.Userdata); err != nil {
		// mock / missing iso tools: log and continue (SSH inject won't work)
		m.log.Warn("cloud-init seed skipped", "err", err)
	}

	if err := m.rt.Start(ctx, inst, diskPath); err != nil {
		return m.fail(inst, fmt.Errorf("start: %w", err))
	}
	inst.Status = vm.StatusRunning
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	// Wait for SSH when not mock and port assigned
	if m.cfg.Hypervisor != "mock" && inst.SSHPort > 0 {
		user := m.cfg.SSHUser
		if user == "" || user == "alpine" {
			if spec, err := image.Get(img); err == nil && spec.SSHUser != "" {
				user = spec.SSHUser
			}
		}
		// also try "grain" user from cloud-init
		waitCtx, cancel := context.WithTimeout(ctx, m.cfg.ReadyTimeout)
		defer cancel()
		if err := guest.WaitSSH(waitCtx, inst.IP, inst.SSHPort, user, priv); err != nil {
			// try grain user
			if err2 := guest.WaitSSH(waitCtx, inst.IP, inst.SSHPort, "grain", priv); err2 != nil {
				m.log.Warn("ssh not ready yet", "name", name, "err", err)
			} else {
				user = "grain"
			}
		}
		_ = user
	}

	m.log.Info("vm created",
		"name", inst.Name,
		"persistent", inst.Persistent,
		"cpus", inst.CPUs,
		"memory_mb", inst.MemoryMB,
		"ssh_port", inst.SSHPort,
	)
	return inst, nil
}

func (m *Manager) fail(inst *vm.Instance, err error) (*vm.Instance, error) {
	inst.Status = vm.StatusError
	inst.Error = err.Error()
	_ = m.st.Put(inst)
	return nil, err
}

func (m *Manager) List() ([]*vm.Instance, error) {
	list, err := m.st.List()
	if err != nil {
		return nil, err
	}
	for _, inst := range list {
		if inst.Status == vm.StatusRunning && !m.rt.Running(inst) {
			inst.Status = vm.StatusStopped
			inst.PID = 0
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
func (m *Manager) Shutdown(ctx context.Context, name string) error {
	inst, err := m.st.Get(name)
	if err != nil {
		return err
	}
	if err := m.rt.Stop(ctx, inst); err != nil {
		return err
	}
	if !inst.Persistent {
		return m.st.Delete(name)
	}
	inst.Status = vm.StatusStopped
	inst.PID = 0
	return m.st.Put(inst)
}

// Stop is an alias for Shutdown.
func (m *Manager) Stop(ctx context.Context, name string) error {
	return m.Shutdown(ctx, name)
}

// Start boots a stopped persistent (or any stored) VM using its existing disk.
func (m *Manager) Start(ctx context.Context, name string) (*vm.Instance, error) {
	inst, err := m.st.Get(name)
	if err != nil {
		return nil, err
	}
	if inst.Status == vm.StatusRunning && m.rt.Running(inst) {
		return nil, fmt.Errorf("vm %q already running", name)
	}
	if inst.DiskPath == "" || !DiskExists(inst.DiskPath) {
		return nil, fmt.Errorf("vm %q has no disk (disk_path missing or gone)", name)
	}

	priv, pub, err := sshkey.Ensure(m.cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("ssh key: %w", err)
	}

	vmDir := m.st.Dir(name)
	seed := filepath.Join(vmDir, "seed.iso")
	if !DiskExists(seed) {
		if _, err := cloudinit.WriteNoCloud(vmDir, name, pub, ""); err != nil {
			m.log.Warn("cloud-init seed skipped", "err", err)
		}
	}

	if err := m.rt.Start(ctx, inst, inst.DiskPath); err != nil {
		return m.fail(inst, fmt.Errorf("start: %w", err))
	}
	inst.Status = vm.StatusRunning
	inst.Error = ""
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	if m.cfg.Hypervisor != "mock" && inst.SSHPort > 0 {
		user := m.cfg.SSHUser
		if user == "" || user == "alpine" {
			if spec, err := image.Get(inst.Image); err == nil && spec.SSHUser != "" {
				user = spec.SSHUser
			}
		}
		waitCtx, cancel := context.WithTimeout(ctx, m.cfg.ReadyTimeout)
		defer cancel()
		if err := guest.WaitSSH(waitCtx, inst.IP, inst.SSHPort, user, priv); err != nil {
			if err2 := guest.WaitSSH(waitCtx, inst.IP, inst.SSHPort, "grain", priv); err2 != nil {
				m.log.Warn("ssh not ready yet", "name", name, "err", err)
			}
		}
	}

	m.log.Info("vm started",
		"name", inst.Name,
		"persistent", inst.Persistent,
		"ssh_port", inst.SSHPort,
	)
	return inst, nil
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

package manager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/names"
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
	image := opts.Image
	if image == "" {
		image = m.cfg.Image
	}

	inst := &vm.Instance{
		Name:       name,
		Status:     vm.StatusCreating,
		Persistent: opts.Persistent,
		CPUs:       cpus,
		MemoryMB:   mem,
		DiskGB:     diskGB,
		Image:      image,
		Tags:       opts.Tags,
		CreatedAt:  time.Now().UTC(),
	}
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}

	base, err := m.disk.EnsureBase(ctx, image)
	if err != nil {
		inst.Status = vm.StatusError
		inst.Error = err.Error()
		_ = m.st.Put(inst)
		return nil, fmt.Errorf("image: %w", err)
	}

	diskPath := filepath.Join(m.st.Dir(name), "disk.img")
	if err := m.disk.Clone(ctx, base, diskPath, diskGB); err != nil {
		inst.Status = vm.StatusError
		inst.Error = err.Error()
		_ = m.st.Put(inst)
		return nil, fmt.Errorf("disk: %w", err)
	}
	inst.DiskPath = diskPath

	if err := m.rt.Start(ctx, inst, diskPath); err != nil {
		inst.Status = vm.StatusError
		inst.Error = err.Error()
		_ = m.st.Put(inst)
		return nil, fmt.Errorf("start: %w", err)
	}
	inst.Status = vm.StatusRunning
	if err := m.st.Put(inst); err != nil {
		return nil, err
	}
	m.log.Info("vm created",
		"name", inst.Name,
		"persistent", inst.Persistent,
		"cpus", inst.CPUs,
		"memory_mb", inst.MemoryMB,
	)
	return inst, nil
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

// Delete stops the VM and removes disk unless keepDisk is true.
// Ephemeral VMs always remove disk; persistent VMs remove meta+disk on delete.
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

// Shutdown stops the hypervisor process. Ephemeral VMs are deleted; persistent keep disk.
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

// CleanupEphemeral removes all non-persistent VMs (daemon start / stop).
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

// DiskPath helper for tests.
func DiskExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

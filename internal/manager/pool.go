package manager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/names"
	"github.com/cxdy/grain/internal/vm"
)

// Pool member tags (persisted on meta.json).
const (
	tagPool         = "grain.pool"
	tagPoolTemplate = "grain.pool.template"
	poolTagValue    = "1"
)

// PoolStatus is the warm pool inventory snapshot.
type PoolStatus struct {
	// Enabled is true when config has a template and size > 0.
	Enabled bool `json:"enabled"`
	// Template is the configured source VM name.
	Template string `json:"template,omitempty"`
	// Desired is warm_pool.size from config.
	Desired int `json:"desired"`
	// Ready is the count of claimable pool members (stopped/suspended, or running when Running mode).
	Ready int `json:"ready"`
	// Members are names of claimable pool VMs (sorted by CreatedAt oldest first in fill/claim).
	Members []string `json:"members,omitempty"`
	// Running is true when warm_pool.running keeps members agent-ready (RAM cost).
	Running bool `json:"running,omitempty"`
}

// PoolStatus returns current warm-pool inventory from config + store.
func (m *Manager) PoolStatus() (PoolStatus, error) {
	wp := m.cfg.WarmPool
	st := PoolStatus{
		Enabled:  wp.Enabled(),
		Template: strings.TrimSpace(wp.Template),
		Desired:  wp.Size,
		Running:  wp.Running,
	}
	list, err := m.listPoolMembers(st.Template)
	if err != nil {
		return st, err
	}
	st.Ready = len(list)
	st.Members = make([]string, 0, len(list))
	for _, inst := range list {
		st.Members = append(st.Members, inst.Name)
	}
	return st, nil
}

// PoolFill clones the configured template until ready count reaches warm_pool.size.
// Template must exist and be persistent stopped/suspended (same rules as Clone).
// Members are left stopped/suspended with grain.pool tags (not started).
func (m *Manager) PoolFill(ctx context.Context) (PoolStatus, error) {
	m.poolMu.Lock()
	defer m.poolMu.Unlock()
	return m.poolFillLocked(ctx)
}

func (m *Manager) poolFillLocked(ctx context.Context) (PoolStatus, error) {
	wp := m.cfg.WarmPool
	if !wp.Enabled() {
		return m.poolStatusUnlocked()
	}
	template := strings.TrimSpace(wp.Template)
	if _, err := m.st.Get(template); err != nil {
		return PoolStatus{}, fmt.Errorf("warm pool template %q: %w", template, err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return PoolStatus{}, err
		}
		ready, err := m.listPoolMembers(template)
		if err != nil {
			return PoolStatus{}, err
		}
		if len(ready) >= wp.Size {
			break
		}
		prefix := poolNamePrefix(template)
		existing, err := m.st.Names()
		if err != nil {
			return PoolStatus{}, err
		}
		for n := range m.creating {
			existing[n] = struct{}{}
		}
		dst := names.Next(prefix, existing)
		inst, err := m.Clone(ctx, template, dst)
		if err != nil {
			return PoolStatus{}, fmt.Errorf("pool fill clone: %w", err)
		}
		// Clone leaves stopped; if template was suspended with marker, status
		// remains stopped but marker is present — claim uses Restore-style start.
		// Prefer suspended status when a snapshot marker exists so operators see intent.
		if tag, ok := readSuspendMarker(m.st.Dir(inst.Name)); ok && tag != "" {
			inst.Status = vm.StatusSuspended
			inst.SuspendedAt = time.Now().UTC()
		}
		if inst.Tags == nil {
			inst.Tags = map[string]string{}
		}
		inst.Tags[tagPool] = poolTagValue
		inst.Tags[tagPoolTemplate] = template
		if err := m.st.Put(inst); err != nil {
			_ = m.Delete(ctx, inst.Name)
			return PoolStatus{}, err
		}
		// Optional running pool: start member now so claim is rename-only.
		if wp.Running {
			started, startErr := m.startFromDisk(ctx, inst)
			if startErr != nil {
				_ = m.Delete(ctx, inst.Name)
				return PoolStatus{}, fmt.Errorf("pool fill start (running mode): %w", startErr)
			}
			// Re-tag after start (start may refresh meta).
			if started.Tags == nil {
				started.Tags = map[string]string{}
			}
			started.Tags[tagPool] = poolTagValue
			started.Tags[tagPoolTemplate] = template
			if err := m.st.Put(started); err != nil {
				return PoolStatus{}, err
			}
			m.log.Info("warm pool filled (running)", "member", started.Name, "template", template)
		} else {
			m.log.Info("warm pool filled", "member", inst.Name, "template", template)
		}
	}
	return m.poolStatusUnlocked()
}

// PoolClaim takes one ready pool member, renames it to destName (or auto),
// clears pool tags, and starts it ( -loadvm when a suspend snapshot exists).
// Asynchronously refills the pool toward warm_pool.size when configured.
func (m *Manager) PoolClaim(ctx context.Context, destName string) (*vm.Instance, error) {
	t0 := time.Now()
	m.poolMu.Lock()
	// Pick and rename under the lock; start outside so other claims can proceed
	// after rename (only one member is removed).
	member, err := m.pickPoolMemberLocked()
	if err != nil {
		m.poolMu.Unlock()
		return nil, err
	}
	// Reserve destination name before rename.
	name, err := m.claimCreateName(strings.TrimSpace(destName))
	if err != nil {
		m.poolMu.Unlock()
		return nil, err
	}
	// If dest equals member name, just untag (unusual but valid).
	var inst *vm.Instance
	if name == member.Name {
		inst = member
		m.releaseCreateName(name)
	} else {
		// Release claim slot: Rename will create newName on disk; claimCreateName
		// already reserved it in creating map so concurrent Create cannot race.
		renamed, renErr := m.st.Rename(member.Name, name)
		m.releaseCreateName(name)
		if renErr != nil {
			m.poolMu.Unlock()
			return nil, renErr
		}
		inst = renamed
	}

	// Clear pool tags so this VM is a normal sandbox.
	if inst.Tags != nil {
		delete(inst.Tags, tagPool)
		delete(inst.Tags, tagPoolTemplate)
		if len(inst.Tags) == 0 {
			inst.Tags = nil
		}
	}
	if err := m.st.Put(inst); err != nil {
		m.poolMu.Unlock()
		return nil, err
	}
	m.poolMu.Unlock()

	tClaimed := time.Now()

	// Running warm pool: member is already agent-ready — skip startFromDisk.
	if m.cfg.WarmPool.Running && (inst.Status == vm.StatusRunning || m.rt.Running(inst)) {
		tDone := time.Now()
		m.log.Info("pool claim timing",
			"name", inst.Name,
			"from", member.Name,
			"loadvm", false,
			"running_mode", true,
			"claim_ms", tClaimed.Sub(t0).Milliseconds(),
			"start_wait_ms", int64(0),
			"total_ms", tDone.Sub(t0).Milliseconds(),
		)
		// Best-effort async refill (same as start path). Detach cancel so refill
		// outlives the claim request while still using a bounded timeout.
		if m.cfg.WarmPool.Enabled() {
			m.poolBG.Add(1)
			go func() {
				defer m.poolBG.Done()
				bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
				defer cancel()
				m.poolMu.Lock()
				defer m.poolMu.Unlock()
				if _, err := m.poolFillLocked(bg); err != nil {
					m.log.Warn("warm pool refill", "err", err)
				}
			}()
		}
		return inst, nil
	}

	hasSnap := false
	if tag, ok := readSuspendMarker(m.st.Dir(inst.Name)); ok {
		inst.LoadVM = tag
		hasSnap = true
	}
	// Align with Spawn: short agent wait is inside startFromDisk readiness.
	started, err := m.startFromDisk(ctx, inst)
	if err != nil {
		return nil, err
	}
	tDone := time.Now()
	m.log.Info("pool claim timing",
		"name", started.Name,
		"from", member.Name,
		"loadvm", hasSnap,
		"claim_ms", tClaimed.Sub(t0).Milliseconds(),
		"start_wait_ms", tDone.Sub(tClaimed).Milliseconds(),
		"total_ms", tDone.Sub(t0).Milliseconds(),
	)

	// Best-effort async refill (do not block claim path). Detach cancel so refill
	// outlives the claim request while still using a bounded timeout.
	if m.cfg.WarmPool.Enabled() {
		m.poolBG.Add(1)
		go func() {
			defer m.poolBG.Done()
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Minute)
			defer cancel()
			m.poolMu.Lock()
			defer m.poolMu.Unlock()
			if _, err := m.poolFillLocked(bg); err != nil {
				m.log.Warn("warm pool refill", "err", err)
			}
		}()
	}
	return started, nil
}

// WaitPoolBackground blocks until async warm-pool refill goroutines finish.
// Intended for tests so TempDir cleanup is not racing clones.
func (m *Manager) WaitPoolBackground() {
	m.poolBG.Wait()
}

// PoolDrain deletes all warm-pool members (optionally filtered by template).
// Empty template uses config template; if still empty, drains every tagged member.
func (m *Manager) PoolDrain(ctx context.Context) (int, error) {
	m.poolMu.Lock()
	defer m.poolMu.Unlock()
	template := strings.TrimSpace(m.cfg.WarmPool.Template)
	list, err := m.listPoolMembers(template)
	if err != nil {
		return 0, err
	}
	// When template is empty in config, listPoolMembers("") returns all pool-tagged.
	n := 0
	for _, inst := range list {
		if err := m.Delete(ctx, inst.Name); err != nil {
			return n, fmt.Errorf("drain %s: %w", inst.Name, err)
		}
		n++
	}
	m.log.Info("warm pool drained", "count", n)
	return n, nil
}

// EnsureWarmPool fills the pool if configured. Safe to call on daemon start.
func (m *Manager) EnsureWarmPool(ctx context.Context) error {
	if !m.cfg.WarmPool.Enabled() {
		return nil
	}
	_, err := m.PoolFill(ctx)
	return err
}

func (m *Manager) poolStatusUnlocked() (PoolStatus, error) {
	wp := m.cfg.WarmPool
	st := PoolStatus{
		Enabled:  wp.Enabled(),
		Template: strings.TrimSpace(wp.Template),
		Desired:  wp.Size,
		Running:  wp.Running,
	}
	list, err := m.listPoolMembers(st.Template)
	if err != nil {
		return st, err
	}
	st.Ready = len(list)
	st.Members = make([]string, 0, len(list))
	for _, inst := range list {
		st.Members = append(st.Members, inst.Name)
	}
	return st, nil
}

func (m *Manager) pickPoolMemberLocked() (*vm.Instance, error) {
	template := strings.TrimSpace(m.cfg.WarmPool.Template)
	list, err := m.listPoolMembers(template)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		// Opportunistic fill when configured, then retry once.
		if m.cfg.WarmPool.Enabled() {
			if _, err := m.poolFillLocked(context.Background()); err != nil {
				return nil, fmt.Errorf("warm pool empty and fill failed: %w", err)
			}
			list, err = m.listPoolMembers(template)
			if err != nil {
				return nil, err
			}
		}
		if len(list) == 0 {
			if !m.cfg.WarmPool.Enabled() {
				return nil, fmt.Errorf("warm pool is empty (set warm_pool.template and warm_pool.size in config, then grain pool fill)")
			}
			return nil, fmt.Errorf("warm pool is empty (template %q)", template)
		}
	}
	// Oldest first (FIFO).
	return list[0], nil
}

// listPoolMembers returns claimable pool VMs, oldest first.
// When template is non-empty, only members tagged for that template are returned.
// When template is empty, all grain.pool-tagged members are returned.
// Default (disk pool): only stopped/suspended members. Running pool mode: only running members.
func (m *Manager) listPoolMembers(template string) ([]*vm.Instance, error) {
	all, err := m.st.List()
	if err != nil {
		return nil, err
	}
	runningMode := m.cfg.WarmPool.Running
	var out []*vm.Instance
	for _, inst := range all {
		if !isPoolMember(inst, template) {
			continue
		}
		if inst.Status == vm.StatusCreating {
			continue
		}
		live := m.rt.Running(inst) || inst.Status == vm.StatusRunning
		if runningMode {
			// Running warm pool: claimable members stay agent-ready.
			if !live && inst.Status != vm.StatusPaused {
				continue
			}
		} else {
			// Disk-only suspended pool: not claimable while running.
			if inst.Status == vm.StatusRunning || inst.Status == vm.StatusPaused {
				continue
			}
			if m.rt.Running(inst) {
				continue
			}
		}
		out = append(out, inst)
	}
	// Sort by CreatedAt ascending (oldest first).
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.Before(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func isPoolMember(inst *vm.Instance, template string) bool {
	if inst == nil || inst.Tags == nil {
		return false
	}
	if inst.Tags[tagPool] != poolTagValue {
		return false
	}
	if template == "" {
		return true
	}
	return inst.Tags[tagPoolTemplate] == template
}

func poolNamePrefix(template string) string {
	t := strings.TrimSpace(template)
	if t == "" {
		t = "sbox"
	}
	// names.Valid max 63: "pool-" + t + "-N" — keep template as-is (already a valid VM name).
	return "pool-" + t
}

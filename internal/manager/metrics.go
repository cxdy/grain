package manager

import (
	"context"
	"fmt"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/metricsring"
	"github.com/cxdy/grain/internal/vm"
)

// MetricsHistory is the API response for GET /vms/{name}/metrics.
type MetricsHistory struct {
	Enabled  bool                 `json:"enabled"`
	Interval string               `json:"interval,omitempty"`
	Points   []metricsring.Sample `json:"points"`
}

// SampleMetrics appends a guest stats snapshot to the host ring when metrics
// are enabled for the VM. No-op when disabled or name missing.
func (m *Manager) SampleMetrics(name string, st *agent.Stats) error {
	if name == "" || st == nil {
		return nil
	}
	inst, err := m.st.Get(name)
	if err != nil || inst == nil || !inst.MetricsEnabled {
		return nil
	}
	cap := m.cfg.SandboxMetricsPoints
	if cap <= 0 {
		cap = metricsring.DefaultCapacity
	}
	path := metricsring.Path(m.st.Dir(name))
	r, err := metricsring.Open(path, cap)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	return r.Append(metricsring.Sample{
		TimeMS:     time.Now().UnixMilli(),
		Load1:      st.Load1,
		MemTotal:   st.MemTotal,
		MemAvail:   st.MemAvail,
		DiskTotal:  st.DiskTotal,
		DiskFree:   st.DiskFree,
		NetRxBytes: st.NetRxBytes,
		NetTxBytes: st.NetTxBytes,
	})
}

// MetricsHistory returns stored samples for a VM (may be empty if never sampled).
// Points are always loaded from disk when present so history survives disable
// and daemon restarts (metrics.ring under the VM dir).
func (m *Manager) MetricsHistory(name string) (*MetricsHistory, error) {
	inst, err := m.st.Get(name)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("sandbox %q not found", name)
	}
	out := &MetricsHistory{
		Enabled:  inst.MetricsEnabled,
		Interval: m.cfg.SandboxMetricsInterval.String(),
		Points:   []metricsring.Sample{},
	}
	cap := m.cfg.SandboxMetricsPoints
	if cap <= 0 {
		cap = metricsring.DefaultCapacity
	}
	path := metricsring.Path(m.st.Dir(name))
	r, err := metricsring.Open(path, cap)
	if err != nil {
		return out, nil // no file yet
	}
	defer func() { _ = r.Close() }()
	pts, err := r.ReadAll()
	if err != nil {
		return out, err
	}
	if pts == nil {
		pts = []metricsring.Sample{}
	}
	out.Points = pts
	return out, nil
}

// StartMetricsSampler polls guest /stats for running VMs with MetricsEnabled
// and appends to each host metrics.ring. Safe to call once per daemon lifetime.
func (m *Manager) StartMetricsSampler(ctx context.Context) {
	if m == nil {
		return
	}
	interval := m.cfg.SandboxMetricsInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	go func() {
		// Stagger first tick so create storms settle.
		t := time.NewTimer(2 * time.Second)
		for {
			select {
			case <-ctx.Done():
				if !t.Stop() {
					select {
					case <-t.C:
					default:
					}
				}
				return
			case <-t.C:
				m.sampleAllMetrics(ctx)
				t.Reset(interval)
			}
		}
	}()
}

func (m *Manager) sampleAllMetrics(ctx context.Context) {
	list, err := m.st.List()
	if err != nil {
		return
	}
	for _, inst := range list {
		if inst == nil || !inst.MetricsEnabled {
			continue
		}
		if inst.Status != vm.StatusRunning && inst.Status != vm.StatusPaused {
			continue
		}
		if !agentTarget(inst).HasEndpoint() {
			continue
		}
		sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		client, err := agent.Dial(sctx, agentTarget(inst))
		if err != nil {
			cancel()
			continue
		}
		st, err := client.Stats(sctx)
		cancel()
		if err != nil || st == nil {
			continue
		}
		_ = m.SampleMetrics(inst.Name, st)
	}
}

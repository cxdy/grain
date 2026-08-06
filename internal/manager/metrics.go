package manager

import (
	"fmt"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/metricsring"
)

// MetricsHistory is the API response for GET /vms/{name}/metrics.
type MetricsHistory struct {
	Enabled  bool                    `json:"enabled"`
	Interval string                  `json:"interval,omitempty"`
	Points   []metricsring.Sample    `json:"points"`
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
	if !inst.MetricsEnabled {
		return out, nil
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

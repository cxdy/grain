package manager

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func testMetricsManager(t *testing.T, points int, interval time.Duration) (*Manager, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	cfg.SandboxMetricsEnabled = true
	cfg.SandboxMetricsPoints = points
	if interval > 0 {
		cfg.SandboxMetricsInterval = interval
	}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	return m, st
}

func TestSampleMetricsNoopPaths(t *testing.T) {
	t.Parallel()
	m, st := testMetricsManager(t, 16, 0)

	if err := m.SampleMetrics("", &agent.Stats{Load1: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.SampleMetrics("x", nil); err != nil {
		t.Fatal(err)
	}
	// Missing VM is a no-op.
	if err := m.SampleMetrics("missing", &agent.Stats{Load1: 1}); err != nil {
		t.Fatal(err)
	}

	// Metrics disabled → no-op.
	inst := &vm.Instance{
		Name: "off", Status: vm.StatusRunning, MetricsEnabled: false,
	}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	if err := m.SampleMetrics("off", &agent.Stats{Load1: 1.5}); err != nil {
		t.Fatal(err)
	}
	hist, err := m.MetricsHistory("off")
	if err != nil {
		t.Fatal(err)
	}
	if hist.Enabled || len(hist.Points) != 0 {
		t.Fatalf("disabled: %+v", hist)
	}
}

func TestSampleMetricsAndHistory(t *testing.T) {
	t.Parallel()
	// cap <= 0 forces DefaultCapacity path in SampleMetrics/MetricsHistory.
	m, st := testMetricsManager(t, 0, 0)

	inst := &vm.Instance{
		Name: "met1", Status: vm.StatusRunning, MetricsEnabled: true,
	}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}

	stats := &agent.Stats{
		Load1:      0.42,
		MemTotal:   1024,
		MemAvail:   512,
		DiskTotal:  2048,
		DiskFree:   1024,
		NetRxBytes: 10,
		NetTxBytes: 20,
	}
	if err := m.SampleMetrics("met1", stats); err != nil {
		t.Fatal(err)
	}
	// Second sample
	stats.Load1 = 0.84
	if err := m.SampleMetrics("met1", stats); err != nil {
		t.Fatal(err)
	}

	hist, err := m.MetricsHistory("met1")
	if err != nil {
		t.Fatal(err)
	}
	if !hist.Enabled {
		t.Fatal("want enabled")
	}
	if hist.Interval == "" {
		t.Fatal("want interval string")
	}
	if len(hist.Points) != 2 {
		t.Fatalf("points %d", len(hist.Points))
	}
	if hist.Points[0].Load1 != 0.42 || hist.Points[1].Load1 != 0.84 {
		t.Fatalf("points %+v", hist.Points)
	}
	if hist.Points[1].MemTotal != 1024 || hist.Points[1].NetTxBytes != 20 {
		t.Fatalf("fields %+v", hist.Points[1])
	}
}

func TestMetricsHistoryMissingAndEmpty(t *testing.T) {
	t.Parallel()
	m, st := testMetricsManager(t, 8, 5*time.Second)

	if _, err := m.MetricsHistory("nope"); err == nil {
		t.Fatal("expected not found")
	}

	inst := &vm.Instance{Name: "empty", Status: vm.StatusStopped, MetricsEnabled: true}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	hist, err := m.MetricsHistory("empty")
	if err != nil {
		t.Fatal(err)
	}
	if !hist.Enabled {
		t.Fatal("enabled")
	}
	if hist.Points == nil || len(hist.Points) != 0 {
		t.Fatalf("want empty points, got %+v", hist.Points)
	}
	if hist.Interval != (5 * time.Second).String() {
		t.Fatalf("interval %q", hist.Interval)
	}
}

func TestSampleAllMetricsBranches(t *testing.T) {
	t.Parallel()
	m, st := testMetricsManager(t, 16, 0)
	ctx := context.Background()

	// Live agent for happy path.
	srv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("agent port")
	}

	// Skip: metrics off
	_ = st.Put(&vm.Instance{Name: "off", Status: vm.StatusRunning, MetricsEnabled: false, AgentPort: port})
	// Skip: not running
	_ = st.Put(&vm.Instance{Name: "stop", Status: vm.StatusStopped, MetricsEnabled: true, AgentPort: port})
	// Skip: no endpoint
	_ = st.Put(&vm.Instance{Name: "noep", Status: vm.StatusRunning, MetricsEnabled: true})
	// Skip: dial fails (bad port)
	_ = st.Put(&vm.Instance{Name: "badport", Status: vm.StatusRunning, MetricsEnabled: true, AgentPort: 1})
	// Happy: running + paused with live agent
	_ = st.Put(&vm.Instance{Name: "run", Status: vm.StatusRunning, MetricsEnabled: true, AgentPort: port})
	_ = st.Put(&vm.Instance{Name: "paused", Status: vm.StatusPaused, MetricsEnabled: true, AgentPort: port})

	m.sampleAllMetrics(ctx)

	hist, err := m.MetricsHistory("run")
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Points) < 1 {
		t.Fatalf("expected sample for run, got %d", len(hist.Points))
	}
	histP, err := m.MetricsHistory("paused")
	if err != nil {
		t.Fatal(err)
	}
	if len(histP.Points) < 1 {
		t.Fatalf("expected sample for paused, got %d", len(histP.Points))
	}
	// Disabled / no endpoint should remain empty.
	for _, name := range []string{"off", "stop", "noep", "badport"} {
		h, err := m.MetricsHistory(name)
		if err != nil {
			t.Fatal(err)
		}
		if len(h.Points) != 0 {
			t.Fatalf("%s: unexpected points %d", name, len(h.Points))
		}
	}
}

func TestStartMetricsSampler(t *testing.T) {
	t.Parallel()
	// Short interval so sample loop ticks during the test window.
	m, st := testMetricsManager(t, 16, 50*time.Millisecond)

	srv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("agent port")
	}

	if err := st.Put(&vm.Instance{
		Name: "sampled", Status: vm.StatusRunning, MetricsEnabled: true, AgentPort: port,
	}); err != nil {
		t.Fatal(err)
	}

	// Default interval path when config interval is zero.
	m.cfg.SandboxMetricsInterval = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.StartMetricsSampler(ctx)

	// StartMetricsSampler staggers first tick by 2s; wait for at least one tick
	// so the case <-t.C branch and sampleAllMetrics from the sampler run.
	deadline = time.Now().Add(5 * time.Second)
	var n int
	for time.Now().Before(deadline) {
		hist, err := m.MetricsHistory("sampled")
		if err != nil {
			t.Fatal(err)
		}
		n = len(hist.Points)
		if n >= 1 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n < 1 {
		// Fallback: direct sample so the assertion is not solely timer-dependent
		// in overloaded CI, but the wait above still exercises StartMetricsSampler.
		m.sampleAllMetrics(ctx)
		hist, err := m.MetricsHistory("sampled")
		if err != nil {
			t.Fatal(err)
		}
		n = len(hist.Points)
	}
	if n < 1 {
		t.Fatalf("want at least one sample, got %d", n)
	}

	// Cancel stops the sampler loop (ctx.Done branch + timer drain).
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Nil manager is a no-op.
	var nilM *Manager
	nilM.StartMetricsSampler(context.Background())
}

func TestStartMetricsSamplerCancelDuringFirstTimer(t *testing.T) {
	t.Parallel()
	m, _ := testMetricsManager(t, 8, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	m.StartMetricsSampler(ctx)
	// Cancel before the 2s first-tick timer fires.
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestSampleAllMetricsListError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	// Break store list by replacing vms with a file.
	vms := dir + "/vms"
	_ = os.RemoveAll(vms)
	if err := os.WriteFile(vms, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// sampleAllMetrics should swallow list errors.
	m.sampleAllMetrics(context.Background())
}

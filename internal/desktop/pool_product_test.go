package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/config"
)

func TestValidateWarmPoolForm(t *testing.T) {
	t.Parallel()
	if err := ValidateWarmPoolForm(WarmPoolForm{Template: "g", Size: 2}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWarmPoolForm(WarmPoolForm{Size: -1}); err == nil {
		t.Fatal("want size error")
	}
	if err := ValidateWarmPoolForm(WarmPoolForm{Size: 33}); err == nil {
		t.Fatal("want size max error")
	}
	if err := ValidateWarmPoolForm(WarmPoolForm{Size: 2}); err == nil {
		t.Fatal("want template required")
	}
	if err := ValidateWarmPoolForm(WarmPoolForm{}); err != nil {
		t.Fatal(err)
	}
}

func TestPatchAndSaveWarmPoolForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: mock\napi: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveWarmPoolForm(path, WarmPoolForm{Template: "golden", Size: 3, Running: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "template: golden") || !strings.Contains(s, "size: 3") {
		t.Fatalf("warm_pool missing: %s", s)
	}
	if !strings.Contains(s, "running: true") {
		t.Fatalf("running: %s", s)
	}
	if !strings.Contains(s, "hypervisor: mock") {
		t.Fatalf("lost hypervisor: %s", s)
	}
	// Full config validate path.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WarmPool.Template != "golden" || cfg.WarmPool.Size != 3 || !cfg.WarmPool.Running {
		t.Fatalf("%+v", cfg.WarmPool)
	}

	got, err := ReadWarmPoolForm(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Template != "golden" || got.Size != 3 || !got.Running {
		t.Fatalf("%+v", got)
	}

	// Disable
	if err := SaveWarmPoolForm(path, WarmPoolForm{}); err != nil {
		t.Fatal(err)
	}
	got, err = ReadWarmPoolForm(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != 0 || got.Template != "" {
		t.Fatalf("want disabled, got %+v", got)
	}
}

func TestPatchWarmPoolYAMLInvalid(t *testing.T) {
	t.Parallel()
	if _, err := PatchWarmPoolYAML("not: [yaml", WarmPoolForm{Template: "g", Size: 1}); err == nil {
		t.Fatal("want parse error")
	}
	if _, err := PatchWarmPoolYAML("", WarmPoolForm{Size: 1}); err == nil {
		t.Fatal("want validate error")
	}
}

func TestDecideDefaultCreateMode(t *testing.T) {
	t.Parallel()
	d := DecideDefaultCreateMode(false, 0, 0, "")
	if d.Mode != "cold" || d.PreferPool {
		t.Fatalf("%+v", d)
	}
	if !strings.Contains(d.Status, "not configured") {
		t.Fatalf("status: %s", d.Status)
	}

	d = DecideDefaultCreateMode(true, 2, 3, "golden")
	if d.Mode != "pool" || !d.PreferPool {
		t.Fatalf("%+v", d)
	}
	if !strings.Contains(d.Status, "ready 2") {
		t.Fatalf("status: %s", d.Status)
	}

	d = DecideDefaultCreateMode(true, 0, 2, "golden")
	if d.Mode != "cold" || d.PreferPool {
		t.Fatalf("%+v", d)
	}
	if !strings.Contains(strings.ToLower(d.Status), "empty") {
		t.Fatalf("status: %s", d.Status)
	}
}

func TestFilterActivityBySources(t *testing.T) {
	t.Parallel()
	ev := []client.ActivityEvent{
		{ID: "1", Source: "cli", Action: "create"},
		{ID: "2", Source: "desktop", Action: "start"},
		{ID: "3", Source: "mcp", Action: "list"},
		{ID: "4", Source: "api", Action: "stop"},
		{ID: "5", Source: "", Action: "x"}, // empty → api
	}
	all := FilterActivityBySources(ev, nil)
	if len(all) != 5 {
		t.Fatalf("all nil: %d", len(all))
	}
	all = FilterActivityBySources(ev, []string{"all"})
	if len(all) != 5 {
		t.Fatalf("all: %d", len(all))
	}
	cli := FilterActivityBySources(ev, []string{"cli"})
	if len(cli) != 1 || cli[0].ID != "1" {
		t.Fatalf("%+v", cli)
	}
	multi := FilterActivityBySources(ev, []string{"Desktop", "MCP"})
	if len(multi) != 2 {
		t.Fatalf("%+v", multi)
	}
	api := FilterActivityBySources(ev, []string{"api"})
	if len(api) != 2 { // id 4 + empty source
		t.Fatalf("%+v", api)
	}
	ui := FilterActivityBySources(ev, []string{"ui"})
	if len(ui) != 1 || ui[0].Source != "desktop" {
		t.Fatalf("ui alias: %+v", ui)
	}
}

func TestBulkStartPreflight(t *testing.T) {
	t.Parallel()
	// All already running → block
	r := BulkStartPreflight(BulkStartPreflightInput{
		ToStart:   []BulkStartVM{{Name: "a", Status: "running", CPUs: 2, MemoryMB: 1024}},
		CapsKnown: true,
		Caps:      ResourceCaps{MaxVMs: 8},
	})
	if !r.Block || r.WouldStart != 0 {
		t.Fatalf("%+v", r)
	}

	// Under limit → ok
	r = BulkStartPreflight(BulkStartPreflightInput{
		ToStart: []BulkStartVM{
			{Name: "a", Status: "stopped", CPUs: 2, MemoryMB: 1024},
			{Name: "b", Status: "suspended", CPUs: 2, MemoryMB: 1024},
		},
		RunningCount:    1,
		RunningCPUs:     2,
		RunningMemoryMB: 2048,
		CapsKnown:       true,
		Caps:            ResourceCaps{MaxVMs: 8, MaxCPUsTotal: 16, MaxMemoryMBTotal: 16384},
	})
	if !r.OK || r.Block || r.WouldStart != 2 || r.ProjectedRunning != 3 {
		t.Fatalf("%+v", r)
	}

	// Over max_vms → block
	r = BulkStartPreflight(BulkStartPreflightInput{
		ToStart: []BulkStartVM{
			{Name: "a", Status: "stopped", CPUs: 1, MemoryMB: 512},
			{Name: "b", Status: "stopped", CPUs: 1, MemoryMB: 512},
		},
		RunningCount: 7,
		CapsKnown:    true,
		Caps:         ResourceCaps{MaxVMs: 8},
	})
	if !r.Block || !strings.Contains(r.Message, "max_vms") {
		t.Fatalf("%+v", r)
	}

	// Over memory → block
	r = BulkStartPreflight(BulkStartPreflightInput{
		ToStart:         []BulkStartVM{{Name: "a", Status: "stopped", CPUs: 1, MemoryMB: 4096}},
		RunningMemoryMB: 4096,
		CapsKnown:       true,
		Caps:            ResourceCaps{MaxMemoryMBTotal: 6000},
	})
	if !r.Block || !strings.Contains(r.Message, "max_memory") {
		t.Fatalf("%+v", r)
	}

	// Caps unknown → warn, allow
	r = BulkStartPreflight(BulkStartPreflightInput{
		ToStart:   []BulkStartVM{{Name: "a", Status: "stopped", CPUs: 2, MemoryMB: 2048}},
		CapsKnown: false,
	})
	if r.Block || !r.Warn || !r.OK {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.Message, "not available") {
		t.Fatalf("msg: %s", r.Message)
	}

	// Large batch warn
	vms := make([]BulkStartVM, 12)
	for i := range vms {
		vms[i] = BulkStartVM{Name: "x", Status: "stopped", CPUs: 1, MemoryMB: 256}
	}
	r = BulkStartPreflight(BulkStartPreflightInput{
		ToStart:   vms,
		CapsKnown: true,
		Caps:      ResourceCaps{MaxVMs: 100},
	})
	if !r.Warn || r.Block {
		t.Fatalf("%+v", r)
	}
}

func TestPlanPromoteGolden(t *testing.T) {
	t.Parallel()
	if _, err := PlanPromoteGolden("", "running", 0, false); err == nil {
		t.Fatal("want name error")
	}
	p, err := PlanPromoteGolden("golden", "running", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Template != "golden" || p.Size != 2 || !p.NeedSuspend {
		t.Fatalf("%+v", p)
	}
	p, err = PlanPromoteGolden("g", "suspended", 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.NeedSuspend || p.Size != 4 || !p.Running {
		t.Fatalf("%+v", p)
	}
}

func TestResourceCapsFromConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.MaxVMs = 4
	cfg.MaxCPUsTotal = 16
	cfg.MaxMemoryMBTotal = 8192
	c := ResourceCapsFromConfig(cfg)
	if c.MaxVMs != 4 || c.MaxCPUsTotal != 16 || c.MaxMemoryMBTotal != 8192 {
		t.Fatalf("%+v", c)
	}
}

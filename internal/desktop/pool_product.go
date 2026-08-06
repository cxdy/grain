package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/config"
	"gopkg.in/yaml.v3"
)

// WarmPoolForm is the Settings / promote payload for warm_pool.
type WarmPoolForm struct {
	Template string `json:"template"`
	Size     int    `json:"size"`
	// Running keeps pool members agent-ready (uses host RAM). Default false = disk-only suspended pool.
	Running bool `json:"running"`
}

// ValidateWarmPoolForm checks form fields against the same rules as config.Validate.
func ValidateWarmPoolForm(f WarmPoolForm) error {
	if f.Size < 0 {
		return fmt.Errorf("warm_pool.size must be non-negative")
	}
	if f.Size > 32 {
		return fmt.Errorf("warm_pool.size must be <= 32")
	}
	tpl := strings.TrimSpace(f.Template)
	if f.Size > 0 && tpl == "" {
		return fmt.Errorf("warm_pool.template is required when warm_pool.size > 0")
	}
	return nil
}

// SaveWarmPoolForm merges warm_pool into config.yaml (preserves other keys).
// Does not restart the daemon — caller should SaveConfigFile restart or grain down/up.
func SaveWarmPoolForm(configPath string, form WarmPoolForm) error {
	if err := ValidateWarmPoolForm(form); err != nil {
		return err
	}
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		configPath = filepath.Join(home, ".grain", "config.yaml")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	patched, err := PatchWarmPoolYAML(string(raw), form)
	if err != nil {
		return err
	}
	// Validate via full config parse when possible.
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(configPath), "grain-warm-pool-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(patched); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := config.ValidateFile(tmpPath); err != nil {
		return err
	}
	if !strings.HasSuffix(patched, "\n") {
		patched += "\n"
	}
	return os.WriteFile(configPath, []byte(patched), 0o600)
}

// PatchWarmPoolYAML updates warm_pool.template/size/running in a YAML document string.
func PatchWarmPoolYAML(raw string, form WarmPoolForm) (string, error) {
	if err := ValidateWarmPoolForm(form); err != nil {
		return "", err
	}
	var doc map[string]interface{}
	if strings.TrimSpace(raw) != "" {
		if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
			return "", fmt.Errorf("parse config: %w", err)
		}
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	wp, _ := doc["warm_pool"].(map[string]interface{})
	if wp == nil {
		wp = map[string]interface{}{}
	}
	tpl := strings.TrimSpace(form.Template)
	if tpl == "" && form.Size == 0 {
		// Explicit disable: clear template and size.
		wp["template"] = ""
		wp["size"] = 0
	} else {
		wp["template"] = tpl
		wp["size"] = form.Size
	}
	if form.Running {
		wp["running"] = true
	} else {
		// Keep YAML clean: omit or set false.
		wp["running"] = false
	}
	doc["warm_pool"] = wp
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	s := string(out)
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s, nil
}

// ReadWarmPoolForm extracts warm_pool from a config file (defaults empty/disabled).
func ReadWarmPoolForm(configPath string) (WarmPoolForm, error) {
	var f WarmPoolForm
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return f, err
		}
		configPath = filepath.Join(home, ".grain", "config.yaml")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return f, nil
		}
		return f, err
	}
	var doc struct {
		WarmPool struct {
			Template string `yaml:"template"`
			Size     int    `yaml:"size"`
			Running  bool   `yaml:"running"`
		} `yaml:"warm_pool"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return f, err
	}
	return WarmPoolForm{
		Template: strings.TrimSpace(doc.WarmPool.Template),
		Size:     doc.WarmPool.Size,
		Running:  doc.WarmPool.Running,
	}, nil
}

// CreateModeDecision is the default New-sandbox mode + honest status copy.
type CreateModeDecision struct {
	// Mode is "pool" when prefer-pool applies, else "cold".
	Mode string `json:"mode"`
	// PreferPool is true when ready > 0 and pool is enabled.
	PreferPool bool `json:"prefer_pool"`
	// Status is human-readable status for the create UI.
	Status string `json:"status"`
	// Hint is short mode-line guidance.
	Hint string `json:"hint"`
	// Ready / Desired / Enabled / Template mirror pool status fields used for the decision.
	Ready    int    `json:"ready"`
	Desired  int    `json:"desired"`
	Enabled  bool   `json:"enabled"`
	Template string `json:"template,omitempty"`
}

// DecideDefaultCreateMode picks pool when ready>0, else cold with honest empty/unconfigured copy.
// running distinguishes suspended (disk-only) vs running (agent-ready, RAM) pool members.
func DecideDefaultCreateMode(enabled bool, ready, desired int, template string) CreateModeDecision {
	return DecideDefaultCreateModeRunning(enabled, ready, desired, template, false)
}

// DecideDefaultCreateModeRunning is DecideDefaultCreateMode with suspended vs running honesty.
func DecideDefaultCreateModeRunning(enabled bool, ready, desired int, template string, running bool) CreateModeDecision {
	tpl := strings.TrimSpace(template)
	d := CreateModeDecision{
		Mode:     "cold",
		Ready:    ready,
		Desired:  desired,
		Enabled:  enabled,
		Template: tpl,
	}
	modeWord := "suspended (disk-only)"
	if running {
		modeWord = "running (agent-ready, uses host RAM)"
	}
	if !enabled {
		d.Status = "Warm pool is not configured. Cold boot waits for guest agent (~seconds). Settings → Warm pool: set template + size, or More → Promote to golden + fill."
		d.Hint = "Cold boot waits for guest agent (~seconds). Template/pool use suspend snapshots when available."
		return d
	}
	if ready > 0 {
		d.Mode = "pool"
		d.PreferPool = true
		d.Status = fmt.Sprintf("Pool %q — ready %d / desired %d · %s. New prefers claim (fast).", tpl, ready, desired, modeWord)
		if running {
			d.Hint = "Claims a running pool member (rename-only) — fastest assign path."
		} else {
			d.Hint = "Claims a pre-cloned suspended member and starts it (−loadvm when snapshotted)."
		}
		return d
	}
	// Enabled but empty.
	d.Status = fmt.Sprintf("Pool %q is empty (ready 0 / desired %d · %s). Fill the pool first for fast claim, or cold-boot now.", tpl, desired, modeWord)
	d.Hint = "Pool empty — Settings → Fill pool, then New prefers claim."
	return d
}

// FormatWarmPathChecklist is a short operator loop summary for Settings / create UI.
func FormatWarmPathChecklist(enabled bool, ready, desired int, template string, running bool) string {
	tpl := strings.TrimSpace(template)
	var steps []string
	if !enabled || tpl == "" {
		steps = append(steps, "1. Promote a ready sandbox (More → Promote to golden + fill) or set template in Settings")
		steps = append(steps, "2. Set desired size ≥ 1 and Apply (restarts local daemon)")
		steps = append(steps, "3. Fill pool, then New sandbox (prefers claim when ready > 0)")
		return strings.Join(steps, "\n")
	}
	kind := "suspended members"
	if running {
		kind = "running members (RAM)"
	}
	steps = append(steps, fmt.Sprintf("Template %q · size %d · %s", tpl, desired, kind))
	if ready > 0 {
		steps = append(steps, fmt.Sprintf("Ready %d — New sandbox will prefer From warm pool", ready))
	} else {
		steps = append(steps, "Ready 0 — click Fill pool, then New")
	}
	return strings.Join(steps, "\n")
}

// ResourceCapsFromInfoMap parses GET /info string map for resource caps.
// Returns CapsKnown true when at least one cap key is present (including explicit 0).
func ResourceCapsFromInfoMap(info map[string]string) (ResourceCaps, bool) {
	if info == nil {
		return ResourceCaps{}, false
	}
	keys := []string{"max_vms", "max_cpus_total", "max_memory_mb_total"}
	known := false
	var c ResourceCaps
	for _, k := range keys {
		if _, ok := info[k]; ok {
			known = true
			break
		}
	}
	if !known {
		return ResourceCaps{}, false
	}
	c.MaxVMs = atoiDefault(info["max_vms"], 0)
	c.MaxCPUsTotal = atoiDefault(info["max_cpus_total"], 0)
	c.MaxMemoryMBTotal = atoiDefault(info["max_memory_mb_total"], 0)
	return c, true
}

func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n := 0
	neg := false
	for i, r := range s {
		if i == 0 && r == '-' {
			neg = true
			continue
		}
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		return -n
	}
	return n
}

// FilterActivityBySources returns events whose Source is in sources (case-insensitive).
// Empty sources, or a single entry "all", returns events unchanged (copy).
func FilterActivityBySources(events []client.ActivityEvent, sources []string) []client.ActivityEvent {
	if len(events) == 0 {
		return nil
	}
	if len(sources) == 0 {
		out := make([]client.ActivityEvent, len(events))
		copy(out, events)
		return out
	}
	wantAll := false
	set := make(map[string]struct{}, len(sources))
	for _, s := range sources {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || s == "all" {
			wantAll = true
			continue
		}
		// Normalize common aliases.
		switch s {
		case "api-equivalent", "api":
			s = "api"
		case "ui":
			s = "desktop"
		}
		set[s] = struct{}{}
	}
	if wantAll || len(set) == 0 {
		out := make([]client.ActivityEvent, len(events))
		copy(out, events)
		return out
	}
	var out []client.ActivityEvent
	for _, e := range events {
		src := strings.ToLower(strings.TrimSpace(e.Source))
		if src == "" {
			src = "api"
		}
		if _, ok := set[src]; ok {
			out = append(out, e)
		}
	}
	return out
}

// BulkStartVM is one sandbox considered for bulk start.
type BulkStartVM struct {
	Name     string
	Status   string
	CPUs     int
	MemoryMB int
}

// ResourceCaps are host/daemon limits (0 = unlimited).
type ResourceCaps struct {
	MaxVMs           int
	MaxCPUsTotal     int
	MaxMemoryMBTotal int
}

// BulkStartPreflightInput is inventory + caps for capacity math.
type BulkStartPreflightInput struct {
	// ToStart are selected sandboxes (may include already-running; filtered inside).
	ToStart []BulkStartVM
	// Currently running totals (not including ToStart that are still stopped).
	RunningCount    int
	RunningCPUs     int
	RunningMemoryMB int
	Caps            ResourceCaps
	// CapsKnown is false when Desktop cannot read remote host caps (soft warn only).
	CapsKnown bool
}

// BulkStartPreflightResult is the gate before multi-start fan-out.
type BulkStartPreflightResult struct {
	OK               bool   `json:"ok"`
	Block            bool   `json:"block"`
	Warn             bool   `json:"warn"`
	Message          string `json:"message"`
	WouldStart       int    `json:"would_start"`
	ProjectedRunning int    `json:"projected_running"`
	ProjectedCPUs    int    `json:"projected_cpus"`
	ProjectedMemory  int    `json:"projected_memory_mb"`
	AlreadyRunning   int    `json:"already_running"`
}

// BulkStartPreflight estimates capacity impact of starting stopped sandboxes in a batch.
// Blocks when hard caps would be exceeded; warns when CapsKnown is false or large batch.
func BulkStartPreflight(in BulkStartPreflightInput) BulkStartPreflightResult {
	var toStart []BulkStartVM
	already := 0
	for _, vm := range in.ToStart {
		st := strings.ToLower(strings.TrimSpace(vm.Status))
		if st == "running" || st == "paused" {
			already++
			continue
		}
		toStart = append(toStart, vm)
	}
	res := BulkStartPreflightResult{
		OK:             true,
		WouldStart:     len(toStart),
		AlreadyRunning: already,
	}
	if len(toStart) == 0 {
		res.Message = "All selected sandboxes are already running."
		res.OK = false
		res.Block = true
		return res
	}

	addCPU, addMem := 0, 0
	for _, vm := range toStart {
		cpus := vm.CPUs
		if cpus <= 0 {
			cpus = 2
		}
		mem := vm.MemoryMB
		if mem <= 0 {
			mem = 2048
		}
		addCPU += cpus
		addMem += mem
	}
	res.ProjectedRunning = in.RunningCount + len(toStart)
	res.ProjectedCPUs = in.RunningCPUs + addCPU
	res.ProjectedMemory = in.RunningMemoryMB + addMem

	var blocks []string
	var warns []string

	if in.CapsKnown {
		if in.Caps.MaxVMs > 0 && res.ProjectedRunning > in.Caps.MaxVMs {
			blocks = append(blocks, fmt.Sprintf(
				"would reach %d running VMs but max_vms is %d (currently %d running, starting %d)",
				res.ProjectedRunning, in.Caps.MaxVMs, in.RunningCount, len(toStart)))
		}
		if in.Caps.MaxCPUsTotal > 0 && res.ProjectedCPUs > in.Caps.MaxCPUsTotal {
			blocks = append(blocks, fmt.Sprintf(
				"would use %d total vCPUs but max_cpus_total is %d",
				res.ProjectedCPUs, in.Caps.MaxCPUsTotal))
		}
		if in.Caps.MaxMemoryMBTotal > 0 && res.ProjectedMemory > in.Caps.MaxMemoryMBTotal {
			blocks = append(blocks, fmt.Sprintf(
				"would use %d MiB total memory but max_memory_mb_total is %d",
				res.ProjectedMemory, in.Caps.MaxMemoryMBTotal))
		}
	} else {
		warns = append(warns, "host resource caps (max_vms / memory) are not available for this connection; bulk start may still fail mid-batch on the remote daemon")
	}
	if len(toStart) >= 10 {
		warns = append(warns, fmt.Sprintf("large bulk start (%d VMs) may stress the host even under caps", len(toStart)))
	}

	if len(blocks) > 0 {
		res.OK = false
		res.Block = true
		res.Message = "Bulk start blocked: " + strings.Join(blocks, "; ") +
			". Reduce selection or raise caps in config (max_vms / max_cpus_total / max_memory_mb_total)."
		return res
	}
	if len(warns) > 0 {
		res.Warn = true
		res.Message = fmt.Sprintf("Starting %d sandbox(es) (projected running %d). Warning: %s",
			len(toStart), res.ProjectedRunning, strings.Join(warns, "; "))
		return res
	}
	res.Message = fmt.Sprintf("Starting %d sandbox(es) (projected running %d, %d MiB, %d vCPUs).",
		len(toStart), res.ProjectedRunning, res.ProjectedMemory, res.ProjectedCPUs)
	return res
}

// ResourceCapsFromConfig extracts caps from a full grain config (0 = unlimited).
func ResourceCapsFromConfig(cfg config.Config) ResourceCaps {
	return ResourceCaps{
		MaxVMs:           cfg.MaxVMs,
		MaxCPUsTotal:     cfg.MaxCPUsTotal,
		MaxMemoryMBTotal: cfg.MaxMemoryMBTotal,
	}
}

// PromoteGoldenPlan describes the steps for one-click promote+fill (pure).
type PromoteGoldenPlan struct {
	Template    string `json:"template"`
	Size        int    `json:"size"`
	NeedSuspend bool   `json:"need_suspend"`
	Running     bool   `json:"running"`
	Message     string `json:"message"`
}

// PlanPromoteGolden validates promote inputs and decides suspend need.
// currentSize 0 defaults desired pool size to 2.
func PlanPromoteGolden(sandboxName, status string, currentSize int, runningPool bool) (PromoteGoldenPlan, error) {
	name := strings.TrimSpace(sandboxName)
	if name == "" {
		return PromoteGoldenPlan{}, fmt.Errorf("sandbox name is required to promote as golden template")
	}
	size := currentSize
	if size <= 0 {
		size = 2
	}
	if size > 32 {
		return PromoteGoldenPlan{}, fmt.Errorf("warm_pool.size must be <= 32")
	}
	st := strings.ToLower(strings.TrimSpace(status))
	needSuspend := st == "running" || st == "paused"
	msg := fmt.Sprintf("Set warm_pool.template=%q size=%d", name, size)
	if needSuspend {
		msg += ", suspend first for loadvm snapshot"
	}
	if runningPool {
		msg += " (running pool mode)"
	}
	return PromoteGoldenPlan{
		Template:    name,
		Size:        size,
		NeedSuspend: needSuspend,
		Running:     runningPool,
		Message:     msg,
	}, nil
}

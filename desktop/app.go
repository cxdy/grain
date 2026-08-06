package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cxdy/grain/internal/desktop"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound surface. All control-plane work goes through
// internal/desktop.Service (public grain client) — not an embedded daemon.
type App struct {
	ctx context.Context
	mu  sync.Mutex
	svc *desktop.Service

	// configPath is the grain config file (default ~/.grain/config.yaml).
	configPath string
	// shellOnlyVM when set launches the UI focused on a single shell session.
	shellOnlyVM string
}

// ShellOnlyVM returns the VM name when running in pop-out shell mode.
func (a *App) ShellOnlyVM() string {
	return a.shellOnlyVM
}

// NewApp constructs the application binding.
func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	_ = a.reloadService()
}

func (a *App) shutdown(ctx context.Context) {}

func (a *App) reloadService() error {
	path := a.configPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".grain", "config.yaml")
	}
	cfg, err := desktop.LoadConfig(path)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.svc = desktop.NewService(cfg)
	a.configPath = path
	return nil
}

func (a *App) service() (*desktop.Service, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.svc == nil {
		return nil, fmt.Errorf("desktop service not initialized")
	}
	return a.svc, nil
}

// SplashResult is returned by EnsureReady for the splash screen.
type SplashResult struct {
	Started        bool                 `json:"started"`
	AlreadyHealthy bool                 `json:"already_healthy"`
	Message        string               `json:"message"`
	Health         desktop.HealthStatus `json:"health"`
	Error          string               `json:"error,omitempty"`
}

// EnsureReady splash-starts the local daemon when needed, then health-checks.
// Always returns a result suitable for the UI; hard dial failures set Error
// without blocking list refresh on the frontend.
func (a *App) EnsureReady() (SplashResult, error) {
	svc, err := a.service()
	if err != nil {
		return SplashResult{Error: err.Error()}, nil
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	res, hs, err := svc.EnsureReady(ctx)
	out := SplashResult{
		Started:        res.Started,
		AlreadyHealthy: res.AlreadyHealthy,
		Message:        res.Message,
		Health:         hs,
	}
	if err != nil {
		out.Error = err.Error()
		if out.Message == "" {
			out.Message = err.Error()
		}
		// Soft error — frontend still loads the shell; do not fail the call.
		return out, nil
	}
	return out, nil
}

// Health returns daemon health for the active connection.
func (a *App) Health() (desktop.HealthStatus, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.HealthStatus{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.Health(ctx)
}

// ListConnections returns named connection profiles.
func (a *App) ListConnections() ([]desktop.Connection, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	return svc.Connections(), nil
}

// GetActiveConnection returns the active profile name.
func (a *App) GetActiveConnection() (string, error) {
	svc, err := a.service()
	if err != nil {
		return "", err
	}
	return svc.Active, nil
}

// SetActiveConnection switches profiles.
func (a *App) SetActiveConnection(name string) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	return svc.SetActive(name)
}

// ListSandboxes returns VMs from the daemon List API.
func (a *App) ListSandboxes() ([]desktop.Sandbox, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.ListSandboxes(ctx)
}

// CreateSandbox creates a VM.
func (a *App) CreateSandbox(opts desktop.CreateOpts) (*desktop.Sandbox, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.CreateSandbox(ctx, opts)
}

// StartSandbox boots a stopped VM.
func (a *App) StartSandbox(name string) (*desktop.Sandbox, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.StartSandbox(ctx, name)
}

// StopSandbox shuts down a VM.
func (a *App) StopSandbox(name string) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.StopSandbox(ctx, name)
}

// RemoveSandbox deletes a VM.
func (a *App) RemoveSandbox(name string) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.RemoveSandbox(ctx, name)
}

// GetSandbox returns one VM.
func (a *App) GetSandbox(name string) (*desktop.Sandbox, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.GetSandbox(ctx, name)
}

// ShellSession returns websocket dial info for xterm.js.
func (a *App) ShellSession(vm string, cols, rows int) (desktop.ShellSessionInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.ShellSessionInfo{}, err
	}
	return svc.ShellSession(vm, cols, rows)
}

// ReadLogs returns serial or qemu logs for a local sandbox.
func (a *App) ReadLogs(vm string, source string) (desktop.ReadLogsResult, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.ReadLogsResult{}, err
	}
	src := desktop.LogSerial
	if source == "qemu" {
		src = desktop.LogQEMU
	}
	return svc.ReadSandboxLogs(vm, src, 0)
}

// ConfigDefaults returns create form defaults from config.
func (a *App) ConfigDefaults() (map[string]interface{}, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"image":     svc.Config.Image,
		"cpus":      svc.Config.DefaultCPUs,
		"memory_mb": svc.Config.DefaultMemoryMB,
		"disk_gb":   svc.Config.DefaultDiskGB,
	}, nil
}

// GetConfigSummary returns a token-masked view of the loaded config for Settings.
func (a *App) GetConfigSummary() (desktop.ConfigSummary, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.ConfigSummary{}, err
	}
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	return svc.Summary(path), nil
}

// GetConfigRaw returns the raw config.yaml text for the editor.
func (a *App) GetConfigRaw() (string, error) {
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	return desktop.ReadConfigFile(path)
}

// SaveConfig validates content with grain check-config, writes the file, restarts
// the local daemon, and reloads the in-app service.
func (a *App) SaveConfig(content string) (desktop.SaveConfigResult, error) {
	a.mu.Lock()
	path := a.configPath
	svc := a.svc
	a.mu.Unlock()
	var runner desktop.CommandRunner = desktop.ExecRunner{}
	if svc != nil && svc.Runner != nil {
		runner = svc.Runner
	}
	res, err := desktop.SaveConfigFile(path, content, true, runner)
	if err != nil {
		return res, err
	}
	if err := a.reloadService(); err != nil {
		res.Message += " (reload in-app config failed: " + err.Error() + ")"
	}
	return res, nil
}

// ValidateName checks a sandbox name against daemon rules.
func (a *App) ValidateName(name string) error {
	return desktop.ValidateSandboxName(name)
}

// ReloadConfig re-reads ~/.grain/config.yaml.
func (a *App) ReloadConfig() error {
	return a.reloadService()
}

// ShellPopOut opens a system terminal running `grain sh <vm>` (reliable pop-out).
func (a *App) ShellPopOut(vm string) error {
	if vm == "" {
		return fmt.Errorf("vm name required")
	}
	grain, err := exec.LookPath("grain")
	if err != nil {
		// try next to this binary
		if exe, e2 := os.Executable(); e2 == nil {
			cand := filepath.Join(filepath.Dir(exe), "grain")
			if _, e3 := os.Stat(cand); e3 == nil {
				grain = cand
				err = nil
			}
		}
	}
	if err != nil || grain == "" {
		return fmt.Errorf("grain CLI not found on PATH (needed for shell pop-out)")
	}
	script := fmt.Sprintf("%q sh %q; exec \"$SHELL\"", grain, vm)
	// macOS Terminal.app
	if _, err := exec.LookPath("osascript"); err == nil {
		return exec.Command("osascript", "-e",
			fmt.Sprintf(`tell application "Terminal" to do script %q`, script)).Start()
	}
	// Linux
	for _, term := range []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xterm"} {
		if p, err := exec.LookPath(term); err == nil {
			return exec.Command(p, "-e", "bash", "-lc", script).Start()
		}
	}
	return fmt.Errorf("no terminal found for shell pop-out")
}

// ListImages returns catalog + local images for the active data dir.
func (a *App) ListImages() ([]desktop.ImageInfo, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	return desktop.ListImages(svc.Config.DataDir), nil
}

// PullImage downloads a catalog image (local host), emitting pull:progress events.
func (a *App) PullImage(id string) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return desktop.PullImageProgress(ctx, svc.Config.DataDir, id, func(written, total int64) {
		if a.ctx == nil {
			return
		}
		pct := 0
		if total > 0 {
			pct = int(written * 100 / total)
		}
		runtime.EventsEmit(a.ctx, "pull:progress", map[string]interface{}{
			"id": id, "written": written, "total": total, "percent": pct,
		})
	})
}

// ReadyImageIDs returns on-disk image ids for create dropdown.
func (a *App) ReadyImageIDs() ([]string, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	return desktop.ReadyImages(svc.Config.DataDir), nil
}

// AddHost saves a remote connection profile to config.yaml and reloads.
func (a *App) AddHost(h desktop.ConnectionWithMCP) error {
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	if err := desktop.SaveHostConnection(path, h); err != nil {
		return err
	}
	return a.reloadService()
}

// GetSandboxMeta returns meta.json for a VM (local data_dir).
func (a *App) GetSandboxMeta(name string) (desktop.SandboxMeta, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.SandboxMeta{}, err
	}
	meta, _, err := desktop.ReadSandboxMeta(svc.Config.DataDir, name)
	return meta, err
}

// SaveSandboxMeta writes meta.json and may resize disk; returns action hints.
func (a *App) SaveSandboxMeta(name string, patch desktop.SandboxMeta) (desktop.WriteSandboxMetaResult, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.WriteSandboxMetaResult{}, err
	}
	return desktop.WriteSandboxMeta(svc.Config.DataDir, name, patch)
}

// OpenSystemTerminal opens the OS terminal with grain sh (not in-app).
func (a *App) OpenSystemTerminal(vm string) error {
	return a.ShellPopOut(vm)
}

// OpenShellWindow opens a new Grain OS window with --shell (true second window).
func (a *App) OpenShellWindow(vm string) error {
	if vm == "" {
		return fmt.Errorf("vm name required")
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		// .../Grain.app/Contents/MacOS/Grain → .app bundle
		app := filepath.Clean(filepath.Join(filepath.Dir(exe), "..", ".."))
		if strings.HasSuffix(app, ".app") {
			candidates = append(candidates, app)
		}
		// monorepo / build tree relatives
		candidates = append(candidates,
			filepath.Clean(filepath.Join(filepath.Dir(exe), "Grain.app")),
			filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "desktop", "build", "bin", "Grain.app")),
			filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "..", "desktop", "build", "bin", "Grain.app")),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "desktop/build/bin/Grain.app"),
			filepath.Join(wd, "build/bin/Grain.app"),
		)
	}
	seen := map[string]bool{}
	for _, app := range candidates {
		if app == "" || seen[app] {
			continue
		}
		seen[app] = true
		st, err := os.Stat(app)
		if err != nil || !st.IsDir() {
			continue
		}
		// open -n = new instance; --args forwards --shell to the binary
		cmd := exec.Command("open", "-n", app, "--args", "--shell", vm)
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	// Fallback: same binary with --shell (separate process)
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate Grain app for shell window: %w", err)
	}
	cmd := exec.Command(exe, "--shell", vm)
	cmd.Env = append(os.Environ(), "GRAIN_DESKTOP_SHELL="+vm)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// AppInfo is static metadata for the about/header strip.
func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"name":    "Grain",
		"product": "Grain",
		"docs":    "https://grainvm.com",
		"stack":   "wails-v2",
	}
}

// RunDoctor returns host/daemon diagnostic checks for the Doctor page.
func (a *App) RunDoctor() ([]desktop.DoctorCheck, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return desktop.RunDoctorChecks(ctx, svc.Config, svc), nil
}

// GetMCPStatus returns MCP config + listen probe + IDE snippets.
func (a *App) GetMCPStatus() (desktop.MCPStatus, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.MCPStatus{}, err
	}
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	local := true
	if conn, err := svc.ActiveConnection(); err == nil {
		local = conn.IsLocal()
	}
	return desktop.GetMCPStatus(path, svc.Config, local)
}

// SetMCPEnabled updates mcp.enabled in config.yaml and reloads service prefs.
func (a *App) SetMCPEnabled(enabled bool, listen string) error {
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	if err := desktop.SetMCPEnabled(path, enabled, listen); err != nil {
		return err
	}
	return a.reloadService()
}

// DeleteHost removes a named remote connection profile.
func (a *App) DeleteHost(name string) error {
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	if err := desktop.DeleteConnection(path, name); err != nil {
		return err
	}
	return a.reloadService()
}

// SaveSettingsForm writes common Settings form fields and reloads.
func (a *App) SaveSettingsForm(form desktop.SettingsForm) error {
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	if err := desktop.SaveSettingsForm(path, form); err != nil {
		return err
	}
	return a.reloadService()
}

// EnsureMCPLocal tries to start local MCP via `grain up --mcp` when not listening.
func (a *App) EnsureMCPLocal() (desktop.MCPStatus, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.MCPStatus{}, err
	}
	conn, err := svc.ActiveConnection()
	if err != nil {
		return desktop.MCPStatus{}, err
	}
	if !conn.IsLocal() {
		return desktop.MCPStatus{}, fmt.Errorf("cannot start MCP on a remote host from Desktop")
	}
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	st, _ := desktop.GetMCPStatus(path, svc.Config, true)
	if st.Listening {
		return st, nil
	}
	runner := svc.Runner
	if runner == nil {
		runner = desktop.ExecRunner{}
	}
	grain, err := runner.LookPath("grain")
	if err != nil {
		return st, fmt.Errorf("grain not on PATH")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_ = runner.StartBackground(ctx, grain, "up", "--mcp")
	// brief wait then re-probe
	for i := 0; i < 15; i++ {
		time.Sleep(200 * time.Millisecond)
		st, _ = desktop.GetMCPStatus(path, svc.Config, true)
		if st.Listening {
			st.Message = "MCP started"
			return st, nil
		}
	}
	st, _ = desktop.GetMCPStatus(path, svc.Config, true)
	if !st.Listening {
		st.Message = "started grain up --mcp; not listening yet — check Doctor or CLI"
	}
	return st, nil
}

// ProbeHosts returns reachability for all configured connections.
func (a *App) ProbeHosts() ([]desktop.HostProbe, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	dial := svc.Dial
	if dial == nil {
		dial = desktop.DialConnection
	}
	return desktop.ProbeConnections(ctx, svc.Config, dial), nil
}

// TestHostConnection probes an explicit API endpoint (Settings add/edit).
func (a *App) TestHostConnection(api, token string) (desktop.HostProbe, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return desktop.TestConnection(ctx, api, token)
}

// GenerateAPIToken rotates api_token in config and returns it once.
func (a *App) GenerateAPIToken() (desktop.TokenActionResult, error) {
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	res, err := desktop.GenerateAPIToken(path)
	if err != nil {
		return res, err
	}
	_ = a.reloadService()
	return res, nil
}

// RevokeAPIToken clears api_token from config.
func (a *App) RevokeAPIToken() (desktop.TokenActionResult, error) {
	a.mu.Lock()
	path := a.configPath
	a.mu.Unlock()
	res, err := desktop.RevokeAPIToken(path)
	if err != nil {
		return res, err
	}
	_ = a.reloadService()
	return res, nil
}

// OpenDocs opens the Grain docs site in the system browser.
func (a *App) OpenDocs() {
	if a.ctx == nil {
		return
	}
	runtime.BrowserOpenURL(a.ctx, "https://grainvm.com")
}

// GetSandboxMetrics returns host-side metrics history for Overview charts.
func (a *App) GetSandboxMetrics(name string) (desktop.MetricsHistoryDTO, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.MetricsHistoryDTO{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.SandboxMetrics(ctx, name)
}

// DoctorRepair runs an allowlisted repair command (e.g. grain up).
func (a *App) DoctorRepair(command string) (desktop.RepairResult, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RepairResult{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	runner := svc.Runner
	if runner == nil {
		runner = desktop.ExecRunner{}
	}
	return desktop.DoctorRepair(ctx, command, runner)
}

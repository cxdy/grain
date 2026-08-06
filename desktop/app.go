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
	// Match default HTML data-theme=dark so the title bar is not white on first paint.
	a.applyNativeTheme("dark")
}

func (a *App) shutdown(ctx context.Context) {}

// SetNativeTheme updates the OS window chrome (title bar / system appearance)
// and window background to match the Desktop light|dark UI theme.
// Called from the frontend whenever the user toggles or loads a saved theme.
func (a *App) SetNativeTheme(theme string) {
	a.applyNativeTheme(theme)
}

func (a *App) applyNativeTheme(theme string) {
	if a.ctx == nil {
		return
	}
	t := strings.ToLower(strings.TrimSpace(theme))
	switch t {
	case "light":
		runtime.WindowSetLightTheme(a.ctx)
		// Match [data-theme=light] --bg #f7f6f3
		runtime.WindowSetBackgroundColour(a.ctx, 247, 246, 243, 255)
	default:
		runtime.WindowSetDarkTheme(a.ctx)
		// Match [data-theme=dark] --bg #0e100f
		runtime.WindowSetBackgroundColour(a.ctx, 14, 16, 15, 255)
	}
}

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

// ListActivity returns recent control-plane activity from the daemon (all clients).
func (a *App) ListActivity(since string, limit int) ([]desktop.ActivityEvent, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.ListActivity(ctx, since, limit)
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

// PoolStatus returns warm-pool inventory from the daemon.
func (a *App) PoolStatus() (*desktop.PoolStatus, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.PoolStatus(ctx)
}

// PoolFill fills the warm pool to the configured size.
func (a *App) PoolFill() (*desktop.PoolStatus, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Disk clones can take a while.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	return svc.PoolFill(ctx)
}

// ListCreateTemplates returns stopped/suspended persistent VMs for spawn-from.
func (a *App) ListCreateTemplates() ([]desktop.Sandbox, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.ListCreateTemplates(ctx)
}

// ExecOne runs sh -c on one sandbox (progressive multi-host Run UI).
func (a *App) ExecOne(name, command string) (desktop.BulkExecResult, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.BulkExecResult{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return svc.ExecOne(ctx, name, command)
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

// BulkExec runs a shell command on many sandboxes in parallel (agent sh -c).
// Returns one result per name with a ready-to-display "name: output" line.
func (a *App) BulkExec(names []string, command string) ([]desktop.BulkExecResult, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Bound total wait so a hung guest cannot freeze the UI forever.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return svc.BulkExec(ctx, names, command)
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

// DeployAgent installs or updates grain-agent inside a running sandbox (SSH).
func (a *App) DeployAgent(name string) (desktop.DeployAgentResult, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.DeployAgentResult{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.DeployAgent(ctx, name)
}

// ExportSandboxRecipe builds a grain/v1 recipe YAML for the sandbox and prompts
// for a save path (native dialog). YAML is always returned on success so the UI
// can still use it if the dialog is cancelled.
func (a *App) ExportSandboxRecipe(name string) (desktop.ExportRecipeResult, error) {
	var out desktop.ExportRecipeResult
	svc, err := a.service()
	if err != nil {
		return out, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	yamlStr, err := svc.ExportSandboxRecipe(ctx, name)
	if err != nil {
		return out, err
	}
	out.YAML = yamlStr

	if a.ctx == nil {
		return out, nil
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export sandbox recipe",
		DefaultFilename: strings.TrimSpace(name) + ".yaml",
		Filters: []runtime.FileFilter{
			{DisplayName: "Recipe YAML (*.yaml)", Pattern: "*.yaml"},
			{DisplayName: "YAML (*.yml)", Pattern: "*.yml"},
		},
	})
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(path) == "" {
		out.Cancelled = true
		return out, nil
	}
	if err := os.WriteFile(path, []byte(yamlStr), 0o644); err != nil {
		return out, fmt.Errorf("write recipe: %w", err)
	}
	out.Path = path
	return out, nil
}

// ExportSandboxRecipeToLibrary exports create-shaped config into ~/.grain/recipes.
func (a *App) ExportSandboxRecipeToLibrary(name string, overwrite bool) (desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeInfo{}, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.ExportSandboxRecipeToLibrary(ctx, name, overwrite)
}

// ListLibraryRecipes returns installed recipes under ~/.grain/recipes.
func (a *App) ListLibraryRecipes() ([]desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	return svc.ListLibraryRecipes()
}

// GetLibraryRecipeYAML returns recipe YAML for the editor.
func (a *App) GetLibraryRecipeYAML(id string) (string, error) {
	svc, err := a.service()
	if err != nil {
		return "", err
	}
	return svc.GetLibraryRecipeYAML(id)
}

// SaveLibraryRecipeYAML validates and overwrites a library recipe.
func (a *App) SaveLibraryRecipeYAML(id, yamlText string) (desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeInfo{}, err
	}
	return svc.SaveLibraryRecipeYAML(id, yamlText)
}

// DeleteLibraryRecipe removes a library file (not sandboxes).
func (a *App) DeleteLibraryRecipe(id string) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	return svc.DeleteLibraryRecipe(id)
}

// ImportRecipeFilePath imports a recipe from an absolute path.
func (a *App) ImportRecipeFilePath(path string, overwrite bool) (desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeInfo{}, err
	}
	return svc.ImportRecipeFile(path, overwrite)
}

// PickAndImportRecipe opens a file dialog and imports a recipe.
func (a *App) PickAndImportRecipe() (desktop.RecipeInfo, error) {
	var zero desktop.RecipeInfo
	if a.ctx == nil {
		return zero, fmt.Errorf("no UI context")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import recipe YAML",
		Filters: []runtime.FileFilter{
			{DisplayName: "YAML", Pattern: "*.yaml;*.yml"},
		},
	})
	if err != nil {
		return zero, err
	}
	if strings.TrimSpace(path) == "" {
		return zero, fmt.Errorf("cancelled")
	}
	return a.ImportRecipeFilePath(path, false)
}

// PreviewRecipeURL fetches and validates a recipe URL without writing the library.
func (a *App) PreviewRecipeURL(url string) (desktop.RecipeURLPreview, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeURLPreview{}, err
	}
	return svc.PreviewRecipeURL(url)
}

// ConfirmRecipeYAML installs previewed YAML into the library (no VM create).
func (a *App) ConfirmRecipeYAML(yamlText, id string, overwrite bool) (desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeInfo{}, err
	}
	return svc.ConfirmRecipeYAML(yamlText, id, overwrite)
}

// ImportRecipeURL downloads and installs a recipe into the library (no preview).
// Prefer PreviewRecipeURL + ConfirmRecipeYAML for interactive UI.
func (a *App) ImportRecipeURL(url string, overwrite bool) (desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeInfo{}, err
	}
	return svc.ImportRecipeURL(url, overwrite)
}

// SearchOfficialRecipes returns the official catalog index.
func (a *App) SearchOfficialRecipes() ([]desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	return svc.SearchOfficialRecipes()
}

// PreviewOfficialRecipe fetches catalog recipe YAML without installing (online).
// Offline: library YAML if installed, else index description only.
func (a *App) PreviewOfficialRecipe(id string) (desktop.RecipeURLPreview, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeURLPreview{}, err
	}
	return svc.PreviewOfficialRecipe(id)
}

// AddOfficialRecipe installs one catalog recipe into the library.
func (a *App) AddOfficialRecipe(id string, overwrite bool) (desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeInfo{}, err
	}
	return svc.AddOfficialRecipe(id, overwrite)
}

// DeployRecipe creates a sandbox from a library recipe.
func (a *App) DeployRecipe(opts desktop.DeployRecipeOpts) (*desktop.Sandbox, error) {
	svc, err := a.service()
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return svc.DeployRecipe(ctx, opts)
}

// RecipeDeployPreflight returns image/mount/host checks before deploy.
func (a *App) RecipeDeployPreflight(recipeID string) (desktop.DeployPreflight, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.DeployPreflight{}, err
	}
	return svc.RecipeDeployPreflight(recipeID)
}

// SaveRecipeForm builds a recipe from the simple form and saves to the library.
func (a *App) SaveRecipeForm(form desktop.RecipeForm, overwrite bool) (desktop.RecipeInfo, error) {
	svc, err := a.service()
	if err != nil {
		return desktop.RecipeInfo{}, err
	}
	return svc.SaveRecipeForm(form, overwrite)
}

// PreviewRecipeForm returns YAML for the form without saving.
func (a *App) PreviewRecipeForm(form desktop.RecipeForm) (string, error) {
	return desktop.BuildRecipeYAML(form)
}

// EnableSandboxMetrics sets metrics_enabled on the VM meta (host data_dir) and
// samples once so Overview can start filling the ring.
func (a *App) EnableSandboxMetrics(name string, enabled bool) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	meta, _, err := desktop.ReadSandboxMeta(svc.Config.DataDir, name)
	if err != nil {
		return err
	}
	_, err = desktop.WriteSandboxMeta(svc.Config.DataDir, name, desktop.SandboxMeta{
		CPUs:              meta.CPUs,
		MemoryMB:          meta.MemoryMB,
		DiskGB:            meta.DiskGB,
		Image:             meta.Image,
		Persistent:        meta.Persistent,
		MetricsEnabled:    enabled,
		MetricsEnabledSet: true,
	})
	if err != nil {
		return err
	}
	// Best-effort live sample via API if daemon is up
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	_, _ = svc.SandboxMetrics(ctx, name)
	return nil
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

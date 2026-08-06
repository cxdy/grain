package desktop

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cxdy/grain/client"
)

// Sandbox is a UI-facing VM summary (core CLI list fields + extras).
type Sandbox struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Image      string `json:"image"`
	Persistent bool   `json:"persistent"`
	CPUs       int    `json:"cpus"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb,omitempty"`
	SSHPort    int    `json:"ssh_port,omitempty"`
	Error      string `json:"error,omitempty"`
	// AgentOK is true when guest agent /health succeeds; false when checked and down; omitted when not checked.
	AgentOK *bool `json:"agent_ok,omitempty"`
	// AgentVersion from guest health when AgentOK.
	AgentVersion string `json:"agent_version,omitempty"`
}

// CreateOpts are create options for the Desktop create form (including advanced).
type CreateOpts struct {
	Name       string `json:"name"`
	Image      string `json:"image"`
	Persistent bool   `json:"persistent"`
	CPUs       int    `json:"cpus"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	Wait       string `json:"wait"`
	Timeout    string `json:"timeout"`
	// Advanced
	Arch     string `json:"arch"`     // arm64|amd64
	GPU      string `json:"gpu"`      // ""|virtio
	Network  string `json:"network"`  // slirp|overlay
	Userdata string `json:"userdata"` // cloud-init userdata
	// Publish is host:guest[,host:guest] port forwards.
	Publish string `json:"publish"`
	// Mounts is newline or comma separated HOST:GUEST paths.
	Mounts string `json:"mounts"`
}

// HealthStatus is returned by Service.Health.
type HealthStatus struct {
	Healthy    bool   `json:"healthy"`
	Connection string `json:"connection"`
	Local      bool   `json:"local"`
	Message    string `json:"message"`
	API        string `json:"api,omitempty"`
	WarnHTTP   bool   `json:"warn_cleartext_http"`
}

// Service is the Desktop backend: dials the grain API and applies local policy.
type Service struct {
	Config Config
	// Active is the current connection name.
	Active string
	// Client is the live API client; set by Connect / SetClient.
	Client *client.Client
	// Dial opens clients (defaults to DialConnection).
	Dial DialFunc
	// Runner starts grain up (defaults to ExecRunner).
	Runner CommandRunner
	// Sleep is injectable for EnsureReady waits.
	Sleep SleepFunc
	// HealthWait is max wait after grain up (default 30s).
	HealthWait time.Duration
	// HealthPoll is poll interval (default 200ms).
	HealthPoll time.Duration
}

// NewService builds a Service with production defaults.
func NewService(cfg Config) *Service {
	return &Service{
		Config:     cfg,
		Active:     cfg.Desktop.DefaultConnection,
		Dial:       DialConnection,
		Runner:     ExecRunner{},
		Sleep:      time.Sleep,
		HealthWait: 30 * time.Second,
		HealthPoll: 200 * time.Millisecond,
	}
}

// Connections returns named profiles for the UI switcher.
func (s *Service) Connections() []Connection {
	return s.Config.ActiveConnections()
}

// SetActive switches the active connection name (does not dial).
func (s *Service) SetActive(name string) error {
	_, err := s.Config.ConnectionByName(name)
	if err != nil {
		return err
	}
	s.Active = name
	s.Client = nil
	return nil
}

// ActiveConnection returns the resolved active profile.
func (s *Service) ActiveConnection() (Connection, error) {
	return s.Config.ConnectionByName(s.Active)
}

// Connect dials the active connection and stores the client.
func (s *Service) Connect() error {
	conn, err := s.ActiveConnection()
	if err != nil {
		return err
	}
	dial := s.Dial
	if dial == nil {
		dial = DialConnection
	}
	c, err := dial(conn, s.Config)
	if err != nil {
		return err
	}
	s.Client = c
	return nil
}

// ensureClient dials if needed.
func (s *Service) ensureClient() (*client.Client, error) {
	if s.Client != nil {
		return s.Client, nil
	}
	if err := s.Connect(); err != nil {
		return nil, err
	}
	return s.Client, nil
}

// probeHealth dials (or re-dials) and runs Health. Clears Client first when forceRedial.
func (s *Service) probeHealth(ctx context.Context, forceRedial bool) error {
	if forceRedial {
		s.Client = nil
	}
	c, err := s.ensureClient()
	if err != nil {
		return err
	}
	return c.Health(ctx)
}

// Health checks the active daemon.
func (s *Service) Health(ctx context.Context) (HealthStatus, error) {
	conn, err := s.ActiveConnection()
	if err != nil {
		return HealthStatus{}, err
	}
	st := HealthStatus{
		Connection: conn.Name,
		Local:      conn.IsLocal(),
		API:        conn.API,
		WarnHTTP:   WarnCleartextRemote(conn.API),
	}
	if err := s.probeHealth(ctx, false); err != nil {
		st.Healthy = false
		st.Message = err.Error()
		return st, nil
	}
	st.Healthy = true
	st.Message = "ok"
	return st, nil
}

// EnsureReady applies local daemon start policy then health-checks.
func (s *Service) EnsureReady(ctx context.Context) (StartDaemonResult, HealthStatus, error) {
	conn, err := s.ActiveConnection()
	if err != nil {
		return StartDaemonResult{}, HealthStatus{}, err
	}
	healthFn := func(ctx context.Context) error {
		// Re-dial each attempt so a post-start socket appears.
		return s.probeHealth(ctx, true)
	}
	startPref := s.Config.Desktop.StartLocalDaemonEnabled()
	res, err := EnsureLocalDaemon(ctx, conn, startPref, healthFn, s.Runner, s.Sleep, s.HealthWait, s.HealthPoll)
	hs, hErr := s.Health(ctx)
	if err != nil {
		return res, hs, err
	}
	if hErr != nil {
		return res, hs, hErr
	}
	if !hs.Healthy {
		return res, hs, fmt.Errorf("daemon still unhealthy: %s", hs.Message)
	}
	return res, hs, nil
}

// ListSandboxes returns VM summaries from the daemon List API.
// For running VMs, probes guest agent health (short timeout).
func (s *Service) ListSandboxes(ctx context.Context) ([]Sandbox, error) {
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	list, err := c.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Sandbox, 0, len(list))
	for _, inst := range list {
		if inst == nil {
			continue
		}
		sb := instanceToSandbox(inst)
		if inst.Status == client.StatusRunning {
			actx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
			h, aerr := c.AgentHealth(actx, inst.Name)
			cancel()
			ok := aerr == nil
			sb.AgentOK = &ok
			if h != nil && h.AgentVersion != "" {
				sb.AgentVersion = h.AgentVersion
			}
		}
		out = append(out, sb)
	}
	return out, nil
}

// CreateSandbox creates a VM via the public client.
func (s *Service) CreateSandbox(ctx context.Context, opts CreateOpts) (*Sandbox, error) {
	if err := ValidateSandboxName(opts.Name); err != nil {
		return nil, err
	}
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	req, err := buildCreateRequest(opts, s.Config)
	if err != nil {
		return nil, err
	}
	inst, err := c.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	sb := instanceToSandbox(inst)
	return &sb, nil
}

// buildCreateRequest applies service/config defaults to create options.
func buildCreateRequest(opts CreateOpts, cfg Config) (client.CreateRequest, error) {
	req := client.CreateRequest{
		Name:       opts.Name,
		Image:      opts.Image,
		Persistent: opts.Persistent,
		CPUs:       opts.CPUs,
		MemoryMB:   opts.MemoryMB,
		DiskGB:     opts.DiskGB,
		Wait:       opts.Wait,
		Timeout:    opts.Timeout,
		Arch:       opts.Arch,
		GPU:        opts.GPU,
		Network:    opts.Network,
		Userdata:   opts.Userdata,
	}
	if req.Image == "" {
		req.Image = cfg.Image
	}
	if req.CPUs <= 0 {
		req.CPUs = cfg.DefaultCPUs
	}
	if req.MemoryMB <= 0 {
		req.MemoryMB = cfg.DefaultMemoryMB
	}
	if req.DiskGB <= 0 {
		req.DiskGB = cfg.DefaultDiskGB
	}
	if req.Wait == "" {
		req.Wait = client.WaitAuto
	}
	fwds, err := parsePublish(opts.Publish)
	if err != nil {
		return req, err
	}
	req.Forwards = fwds
	mts, err := parseMounts(opts.Mounts)
	if err != nil {
		return req, err
	}
	req.Mounts = mts
	return req, nil
}

func parsePublish(s string) ([]client.PortForward, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []client.PortForward
	for _, part := range splitList(s) {
		a, b, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("publish: want host:guest, got %q", part)
		}
		hp, err1 := strconv.Atoi(strings.TrimSpace(a))
		gp, err2 := strconv.Atoi(strings.TrimSpace(b))
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("publish: invalid ports in %q", part)
		}
		out = append(out, client.PortForward{HostPort: hp, GuestPort: gp})
	}
	return out, nil
}

func parseMounts(s string) ([]client.Mount, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []client.Mount
	for _, part := range splitList(s) {
		// HOST:GUEST — host path may contain colons on Windows; take last colon split for guest
		i := strings.LastIndex(part, ":")
		if i <= 0 || i == len(part)-1 {
			return nil, fmt.Errorf("mounts: want HOST:GUEST, got %q", part)
		}
		out = append(out, client.Mount{
			Host:  strings.TrimSpace(part[:i]),
			Guest: strings.TrimSpace(part[i+1:]),
		})
	}
	return out, nil
}

func splitList(s string) []string {
	s = strings.ReplaceAll(s, "\n", ",")
	var parts []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// StartSandbox boots a stopped VM. Before start, grows the qcow2 if meta disk_gb
// exceeds the image virtual size (covers earlier meta-only edits).
func (s *Service) StartSandbox(ctx context.Context, name string) (*Sandbox, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if s.Config.DataDir != "" {
		if err := ensureMetaDiskGrown(s.Config.DataDir, name); err != nil {
			// Non-fatal: still try start; surface via error only if hard fail on resize.
			if strings.Contains(err.Error(), "disk resize:") {
				return nil, err
			}
		}
	}
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	inst, err := c.Start(ctx, name)
	if err != nil {
		return nil, err
	}
	sb := instanceToSandbox(inst)
	return &sb, nil
}

// ensureMetaDiskGrown resizes the VM disk image when meta disk_gb is larger.
func ensureMetaDiskGrown(dataDir, name string) error {
	meta, raw, err := ReadSandboxMeta(dataDir, name)
	if err != nil {
		return err
	}
	if meta.DiskGB <= 0 {
		return nil
	}
	diskPath := resolveDiskPath(dataDir, name, raw)
	need, _, err := diskNeedsGrow(diskPath, meta.DiskGB)
	if err != nil || !need {
		return err
	}
	if err := resizeDiskGB(context.Background(), diskPath, meta.DiskGB); err != nil {
		return fmt.Errorf("disk resize: %w", err)
	}
	return nil
}

// StopSandbox shuts down a VM.
func (s *Service) StopSandbox(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	c, err := s.ensureClient()
	if err != nil {
		return err
	}
	return c.Stop(ctx, name)
}

// RemoveSandbox deletes a VM.
func (s *Service) RemoveSandbox(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	c, err := s.ensureClient()
	if err != nil {
		return err
	}
	return c.Delete(ctx, name)
}

// GetSandbox returns one VM.
func (s *Service) GetSandbox(ctx context.Context, name string) (*Sandbox, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	inst, err := c.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	sb := instanceToSandbox(inst)
	return &sb, nil
}

// ShellSession builds websocket dial info for the active connection.
func (s *Service) ShellSession(vm string, cols, rows int) (ShellSessionInfo, error) {
	conn, err := s.ActiveConnection()
	if err != nil {
		return ShellSessionInfo{}, err
	}
	return BuildShellSessionCfg(conn, s.Config, vm, cols, rows)
}

// ConfigSummary is a UI-safe view of the loaded config (tokens masked).
type ConfigSummary struct {
	Path            string            `json:"path"`
	DataDir         string            `json:"data_dir"`
	Socket          string            `json:"socket"`
	API             string            `json:"api"`
	APIURL          string            `json:"api_url,omitempty"`
	HasToken        bool              `json:"has_token"`
	Image           string            `json:"image"`
	DefaultCPUs     int               `json:"cpus"`
	DefaultMemoryMB int               `json:"memory_mb"`
	DefaultDiskGB   int               `json:"disk_gb"`
	Connections     []ConnectionBrief `json:"connections"`
	Desktop         DesktopPrefs      `json:"desktop"`
	DialHint        string            `json:"dial_hint,omitempty"`
}

// ConnectionBrief is a connection without secrets.
type ConnectionBrief struct {
	Name     string `json:"name"`
	API      string `json:"api,omitempty"`
	Local    bool   `json:"local"`
	TokenEnv string `json:"token_env,omitempty"`
	HasToken bool   `json:"has_token"`
	Notes    string `json:"notes,omitempty"`
}

// Summary returns a safe config snapshot for the Settings page.
func (s *Service) Summary(configPath string) ConfigSummary {
	conns := s.Config.ActiveConnections()
	briefs := make([]ConnectionBrief, 0, len(conns))
	for _, c := range conns {
		briefs = append(briefs, ConnectionBrief{
			Name:     c.Name,
			API:      c.API,
			Local:    c.IsLocal(),
			TokenEnv: c.TokenEnv,
			HasToken: c.Token != "" || (c.TokenEnv != "" && os.Getenv(c.TokenEnv) != ""),
			Notes:    c.Notes,
		})
	}
	hint := ""
	if conn, err := s.ActiveConnection(); err == nil {
		if t, err := ResolveDialTarget(conn, s.Config); err == nil {
			if t.UseUnix {
				hint = "unix://" + t.Socket
			} else {
				hint = t.BaseURL
			}
		}
	}
	return ConfigSummary{
		Path:            configPath,
		DataDir:         s.Config.DataDir,
		Socket:          s.Config.Socket,
		API:             s.Config.API,
		APIURL:          s.Config.APIURL,
		HasToken:        s.Config.ResolvedAPIToken() != "" || os.Getenv("GRAIN_TOKEN") != "",
		Image:           s.Config.Image,
		DefaultCPUs:     s.Config.DefaultCPUs,
		DefaultMemoryMB: s.Config.DefaultMemoryMB,
		DefaultDiskGB:   s.Config.DefaultDiskGB,
		Connections:     briefs,
		Desktop:         s.Config.Desktop,
		DialHint:        hint,
	}
}

// ReadSandboxLogs loads local serial/qemu logs when the active connection is local.
func (s *Service) ReadSandboxLogs(vm string, source LogSource, maxBytes int64) (ReadLogsResult, error) {
	conn, err := s.ActiveConnection()
	if err != nil {
		return ReadLogsResult{}, err
	}
	if !CanReadLocalLogs(conn) {
		return ReadLogsResult{Missing: true}, fmt.Errorf("logs require a local connection (host serial.log); active %q is remote", conn.Name)
	}
	dd := LogsDataDir(conn, s.Config.DataDir)
	return ReadLogs(dd, vm, source, maxBytes)
}

func instanceToSandbox(inst *client.Instance) Sandbox {
	if inst == nil {
		return Sandbox{}
	}
	return Sandbox{
		Name:       inst.Name,
		Status:     string(inst.Status),
		Image:      inst.Image,
		Persistent: inst.Persistent,
		CPUs:       inst.CPUs,
		MemoryMB:   inst.MemoryMB,
		DiskGB:     inst.DiskGB,
		SSHPort:    inst.SSHPort,
		Error:      inst.Error,
	}
}

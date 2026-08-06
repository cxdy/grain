package desktop

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/recipe"
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
	AgentPort  int    `json:"agent_port,omitempty"`
	IP         string `json:"ip,omitempty"`
	Network    string `json:"network,omitempty"`
	Arch       string `json:"arch,omitempty"`
	GPU        string `json:"gpu,omitempty"`
	PID        int    `json:"pid,omitempty"`
	CreatedAt  string `json:"created_at,omitempty"`
	Error      string `json:"error,omitempty"`
	// AgentOK is true when guest agent /health succeeds; false when checked and down; omitted when not checked.
	AgentOK *bool `json:"agent_ok,omitempty"`
	// AgentVersion from guest health when AgentOK.
	AgentVersion string `json:"agent_version,omitempty"`
	// MetricsEnabled when the host samples guest stats for this sandbox.
	MetricsEnabled bool `json:"metrics_enabled,omitempty"`
	// HasAgentImage is true when the image is expected to ship/support grain-agent
	// (grain-ubuntu, grain-ubuntu-fc, or local has_agent marker).
	HasAgentImage bool `json:"has_agent_image,omitempty"`
	// AgentCheckedAt is when Desktop last probed guest agent health (RFC3339).
	AgentCheckedAt string `json:"agent_checked_at,omitempty"`
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
	// From spawns from a stopped/suspended template (fast loadvm when snapshotted).
	From string `json:"from,omitempty"`
	// FromPool claims a warm-pool member (mutually exclusive with From).
	FromPool bool `json:"from_pool,omitempty"`
	// Advanced
	Arch     string `json:"arch"`     // arm64|amd64
	GPU      string `json:"gpu"`      // ""|virtio
	Network  string `json:"network"`  // slirp|overlay
	Userdata string `json:"userdata"` // cloud-init userdata
	// Publish is host:guest[,host:guest] port forwards.
	Publish string `json:"publish"`
	// Mounts is newline or comma separated HOST:GUEST paths.
	Mounts string `json:"mounts"`
	// MetricsEnabled host-side guest stats ring (daemon default true when unset).
	MetricsEnabled bool `json:"metrics_enabled"`
}

// HealthStatus is returned by Service.Health.
type HealthStatus struct {
	Healthy    bool   `json:"healthy"`
	Connection string `json:"connection"`
	Local      bool   `json:"local"`
	Message    string `json:"message"`
	API        string `json:"api,omitempty"`
	// Version is daemon/grain version from GET /info when healthy.
	Version  string `json:"version,omitempty"`
	WarnHTTP bool   `json:"warn_cleartext_http"`
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
	if c, err := s.ensureClient(); err == nil {
		if info, err := c.Info(ctx); err == nil && info != nil {
			if v := strings.TrimSpace(info["version"]); v != "" {
				if !strings.HasPrefix(v, "v") {
					v = "v" + v
				}
				st.Version = v
			}
		}
	}
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

// ActivityEvent is a daemon control-plane action (mirrors GET /activity).
type ActivityEvent = client.ActivityEvent

// ListActivity returns recent daemon activity (CLI, Desktop, MCP, API).
func (s *Service) ListActivity(ctx context.Context, since string, limit int) ([]ActivityEvent, error) {
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	return c.ListActivity(ctx, since, limit)
}

// PoolStatus is warm-pool inventory (GET /pool).
type PoolStatus = client.PoolStatus

// PoolStatus returns warm pool ready count and members.
func (s *Service) PoolStatus(ctx context.Context) (*PoolStatus, error) {
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	return c.PoolStatus(ctx)
}

// PoolFill clones the configured template until ready == desired.
func (s *Service) PoolFill(ctx context.Context) (*PoolStatus, error) {
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	return c.PoolFill(ctx)
}

// ListCreateTemplates returns stopped/suspended persistent VMs suitable as --from sources.
func (s *Service) ListCreateTemplates(ctx context.Context) ([]Sandbox, error) {
	list, err := s.ListSandboxes(ctx)
	if err != nil {
		return nil, err
	}
	var out []Sandbox
	for _, sb := range list {
		st := strings.ToLower(sb.Status)
		if !sb.Persistent {
			continue
		}
		if st == "stopped" || st == "suspended" || st == "error" {
			out = append(out, sb)
		}
	}
	return out, nil
}

// ExecOne runs sh -c command on a single sandbox (for progressive multi-host UI).
func (s *Service) ExecOne(ctx context.Context, name, command string) (BulkExecResult, error) {
	list, err := s.BulkExec(ctx, []string{name}, command)
	if err != nil {
		return BulkExecResult{}, err
	}
	if len(list) == 0 {
		return BulkExecResult{Name: name, Error: "empty result", Line: name + ": error: empty result"}, nil
	}
	return list[0], nil
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
	checkedAt := time.Now().UTC().Format(time.RFC3339)
	for _, inst := range list {
		if inst == nil {
			continue
		}
		sb := instanceToSandbox(inst)
		// Enrich from local meta when available (network/arch/gpu).
		if s.Config.DataDir != "" {
			if meta, _, merr := ReadSandboxMeta(s.Config.DataDir, inst.Name); merr == nil {
				if sb.Network == "" && meta.Network != "" {
					sb.Network = meta.Network
				}
				if sb.Arch == "" && meta.Arch != "" {
					sb.Arch = meta.Arch
				}
				if sb.GPU == "" && meta.GPU != "" {
					sb.GPU = meta.GPU
				}
				if meta.Image != "" {
					sb.HasAgentImage = ImageSupportsAgent(meta.Image)
				}
			}
		}
		if inst.Status == client.StatusRunning {
			applyAgentProbe(ctx, c, &sb, inst.Name, checkedAt)
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
		Name:           opts.Name,
		From:           strings.TrimSpace(opts.From),
		FromPool:       opts.FromPool,
		Image:          opts.Image,
		Persistent:     opts.Persistent,
		CPUs:           opts.CPUs,
		MemoryMB:       opts.MemoryMB,
		DiskGB:         opts.DiskGB,
		Wait:           opts.Wait,
		Timeout:        opts.Timeout,
		Arch:           opts.Arch,
		GPU:            opts.GPU,
		Network:        opts.Network,
		Userdata:       opts.Userdata,
		MetricsEnabled: opts.MetricsEnabled,
	}
	if req.From != "" && req.FromPool {
		return req, fmt.Errorf("from and from_pool are mutually exclusive")
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

// MetricsHistoryDTO is UI-facing metrics ring data.
type MetricsHistoryDTO struct {
	Enabled  bool               `json:"enabled"`
	Interval string             `json:"interval,omitempty"`
	Points   []MetricsSampleDTO `json:"points"`
}

// MetricsSampleDTO is one chart point.
type MetricsSampleDTO struct {
	TimeMS     int64   `json:"t_ms"`
	Load1      float64 `json:"load1"`
	MemTotal   uint64  `json:"mem_total_bytes"`
	MemAvail   uint64  `json:"mem_available_bytes"`
	DiskTotal  uint64  `json:"disk_total_bytes"`
	DiskFree   uint64  `json:"disk_free_bytes"`
	NetRxBytes uint64  `json:"net_rx_bytes"`
	NetTxBytes uint64  `json:"net_tx_bytes"`
}

// SandboxMetrics fetches host-side metrics history for a VM.
func (s *Service) SandboxMetrics(ctx context.Context, name string) (MetricsHistoryDTO, error) {
	var out MetricsHistoryDTO
	c, err := s.ensureClient()
	if err != nil {
		return out, err
	}
	h, err := c.Metrics(ctx, name)
	if err != nil {
		return out, err
	}
	if h == nil {
		return out, nil
	}
	out.Enabled = h.Enabled
	out.Interval = h.Interval
	out.Points = make([]MetricsSampleDTO, 0, len(h.Points))
	for _, p := range h.Points {
		out.Points = append(out.Points, MetricsSampleDTO{
			TimeMS: p.TimeMS, Load1: p.Load1,
			MemTotal: p.MemTotal, MemAvail: p.MemAvail,
			DiskTotal: p.DiskTotal, DiskFree: p.DiskFree,
			NetRxBytes: p.NetRxBytes, NetTxBytes: p.NetTxBytes,
		})
	}
	return out, nil
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

// BulkExecResult is one sandbox's response to a parallel shell command.
type BulkExecResult struct {
	Name     string `json:"name"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
	// Line is a ready-to-display "name: response" (or name: error=…).
	Line string `json:"line"`
}

// BulkExec runs command via guest agent on each named sandbox in parallel
// (sh -c). Order of results matches names. Command is required and non-empty.
func (s *Service) BulkExec(ctx context.Context, names []string, command string) ([]BulkExecResult, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("at least one sandbox name is required")
	}
	c, err := s.ensureClient()
	if err != nil {
		return nil, err
	}
	// Deduplicate while preserving order.
	seen := map[string]struct{}{}
	var list []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		list = append(list, n)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("at least one sandbox name is required")
	}

	type slot struct {
		i int
		r BulkExecResult
	}
	ch := make(chan slot, len(list))
	for i, name := range list {
		i, name := i, name
		go func() {
			res := BulkExecResult{Name: name}
			// Guest shell: one string command for uname -a etc.
			er, eerr := c.Exec(ctx, name, "sh", "-c", command)
			if eerr != nil {
				res.Error = eerr.Error()
				res.ExitCode = -1
				res.Line = name + ": error: " + res.Error
			} else if er != nil {
				res.Stdout = strings.TrimRight(er.Stdout, "\r\n")
				res.Stderr = strings.TrimRight(er.Stderr, "\r\n")
				res.ExitCode = er.ExitCode
				out := res.Stdout
				if out == "" && res.Stderr != "" {
					out = res.Stderr
				}
				if out == "" {
					out = fmt.Sprintf("(exit %d)", res.ExitCode)
				}
				// Collapse multi-line to first line + … if needed for table-ish display;
				// keep full output after the prefix (user asked name: response).
				res.Line = name + ": " + out
				if res.ExitCode != 0 && res.Stderr != "" && res.Stdout != "" {
					res.Line = name + ": " + res.Stdout + "\n" + name + ": stderr: " + res.Stderr
				}
			} else {
				res.Error = "empty result"
				res.ExitCode = -1
				res.Line = name + ": error: empty result"
			}
			ch <- slot{i: i, r: res}
		}()
	}
	out := make([]BulkExecResult, len(list))
	for range list {
		s := <-ch
		out[s.i] = s.r
	}
	return out, nil
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
	if s.Config.DataDir != "" {
		if meta, _, merr := ReadSandboxMeta(s.Config.DataDir, name); merr == nil {
			if sb.Network == "" && meta.Network != "" {
				sb.Network = meta.Network
			}
			if sb.Arch == "" && meta.Arch != "" {
				sb.Arch = meta.Arch
			}
			if sb.GPU == "" && meta.GPU != "" {
				sb.GPU = meta.GPU
			}
			if meta.Image != "" {
				sb.HasAgentImage = ImageSupportsAgent(meta.Image)
			}
		}
	}
	if inst.Status == client.StatusRunning {
		applyAgentProbe(ctx, c, &sb, name, time.Now().UTC().Format(time.RFC3339))
	}
	return &sb, nil
}

// ExportRecipeResult is YAML (+ optional save path) for a sandbox recipe export.
type ExportRecipeResult struct {
	// YAML is the full recipe document (always set on success).
	YAML string `json:"yaml"`
	// Path is set when the user saved via the Desktop file dialog.
	Path string `json:"path,omitempty"`
	// Cancelled is true when the save dialog was dismissed without a path.
	Cancelled bool `json:"cancelled,omitempty"`
}

// ExportSandboxRecipe builds a portable grain/v1 Sandbox recipe from the live
// instance (create options, mounts, port/socket forwards). Bootstrap steps and
// first-boot userdata are not recoverable from a running VM and are omitted.
// Arch/GPU/network are filled from local meta.json when the API omits them.
func (s *Service) ExportSandboxRecipe(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	c, err := s.ensureClient()
	if err != nil {
		return "", err
	}
	inst, err := c.Get(ctx, name)
	if err != nil {
		return "", err
	}
	if inst == nil {
		return "", fmt.Errorf("sandbox %q not found", name)
	}

	snap := recipe.Snapshot{
		Name:       inst.Name,
		Image:      inst.Image,
		CPUs:       inst.CPUs,
		MemoryMB:   inst.MemoryMB,
		DiskGB:     inst.DiskGB,
		Persistent: inst.Persistent,
	}
	for _, m := range inst.Mounts {
		snap.Mounts = append(snap.Mounts, recipe.Mount{
			Host:  m.Host,
			Guest: m.Guest,
			Tag:   m.Tag,
		})
	}
	for _, fwd := range inst.Forwards {
		snap.Forwards = append(snap.Forwards, recipe.Forward{
			HostPort:  fwd.HostPort,
			GuestPort: fwd.GuestPort,
			Proto:     fwd.Proto,
		})
	}
	for _, sf := range inst.SocketForwards {
		snap.SocketForwards = append(snap.SocketForwards, recipe.SocketForward{
			HostPath:  sf.HostPath,
			GuestPath: sf.GuestPath,
		})
	}

	// Local meta often has arch/gpu/network not present on the API Instance DTO.
	if s.Config.DataDir != "" {
		if meta, _, merr := ReadSandboxMeta(s.Config.DataDir, name); merr == nil {
			if snap.Image == "" && meta.Image != "" {
				snap.Image = meta.Image
			}
			if snap.CPUs == 0 && meta.CPUs > 0 {
				snap.CPUs = meta.CPUs
			}
			if snap.MemoryMB == 0 && meta.MemoryMB > 0 {
				snap.MemoryMB = meta.MemoryMB
			}
			if snap.DiskGB == 0 && meta.DiskGB > 0 {
				snap.DiskGB = meta.DiskGB
			}
			snap.Arch = meta.Arch
			snap.GPU = meta.GPU
			snap.Network = meta.Network
			// Prefer meta persistent only when we have local meta (API already set it).
			if meta.Name != "" {
				snap.Persistent = meta.Persistent
			}
		}
	}

	return recipe.FormatSnapshot(snap)
}

// DeployAgentResult is UI-facing agent deploy output.
type DeployAgentResult struct {
	Name         string `json:"name"`
	Binary       string `json:"binary,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	Message      string `json:"message,omitempty"`
}

// DeployAgent installs/updates grain-agent in a running sandbox over SSH.
func (s *Service) DeployAgent(ctx context.Context, name string) (DeployAgentResult, error) {
	var out DeployAgentResult
	if name == "" {
		return out, fmt.Errorf("name is required")
	}
	c, err := s.ensureClient()
	if err != nil {
		return out, err
	}
	res, err := c.DeployAgent(ctx, name)
	if err != nil {
		return out, err
	}
	out.Name = name
	if res != nil {
		out.Name = res.Name
		out.Binary = res.Binary
		if res.Health != nil {
			out.AgentVersion = res.Health.AgentVersion
		}
	}
	if out.AgentVersion != "" {
		out.Message = fmt.Sprintf("guest agent %s ready", out.AgentVersion)
	} else {
		out.Message = "guest agent deployed"
	}
	return out, nil
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
	sb := Sandbox{
		Name:           inst.Name,
		Status:         string(inst.Status),
		Image:          inst.Image,
		Persistent:     inst.Persistent,
		CPUs:           inst.CPUs,
		MemoryMB:       inst.MemoryMB,
		DiskGB:         inst.DiskGB,
		SSHPort:        inst.SSHPort,
		AgentPort:      inst.AgentPort,
		IP:             inst.IP,
		PID:            inst.PID,
		Error:          inst.Error,
		MetricsEnabled: inst.MetricsEnabled,
		HasAgentImage:  ImageSupportsAgent(inst.Image),
	}
	if !inst.CreatedAt.IsZero() {
		sb.CreatedAt = inst.CreatedAt.UTC().Format(time.RFC3339)
	}
	return sb
}

// ImageSupportsAgent reports whether an image is expected to run grain-agent
// (catalog HasAgent or known grain-ubuntu* ids). Used to gate metrics UI and
// Deploy Agent actions for cloud images without an agent.
func ImageSupportsAgent(imageID string) bool {
	id := strings.TrimSpace(imageID)
	if id == "" {
		return false
	}
	// Fast path for known golden IDs (works offline / remote Desktop).
	switch id {
	case "grain-ubuntu", "grain-ubuntu-fc":
		return true
	case "ubuntu-cloud", "alpine-cloud", "fc-kernel":
		return false
	}
	return strings.HasPrefix(id, "grain-ubuntu")
}

// agentProbeGrace is how long after CreatedAt a failed health check stays
// "still probing" (AgentOK omitted) for agent-capable images. Avoids Desktop
// flashing "not installed" while the guest is still booting to grain-agent.
const agentProbeGrace = 2 * time.Minute

// applyAgentProbe sets AgentOK / AgentVersion / AgentCheckedAt on a running sandbox.
//
//   - success → AgentOK=true
//   - failure on non-agent image → AgentOK=false (need deploy or no agent)
//   - failure on agent image within grace of CreatedAt → leave AgentOK unset (UI: checking…)
//   - failure on agent image after grace → AgentOK=false
func applyAgentProbe(ctx context.Context, c *client.Client, sb *Sandbox, name, checkedAt string) {
	if sb == nil || c == nil {
		return
	}
	actx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	h, aerr := c.AgentHealth(actx, name)
	cancel()
	sb.AgentCheckedAt = checkedAt
	if aerr == nil {
		ok := true
		sb.AgentOK = &ok
		if h != nil && h.AgentVersion != "" {
			sb.AgentVersion = h.AgentVersion
		}
		return
	}
	// Probe failed.
	if !sb.HasAgentImage {
		ok := false
		sb.AgentOK = &ok
		return
	}
	// Agent-capable image: during early boot treat as still checking, not "not installed".
	if withinAgentProbeGrace(sb.CreatedAt, time.Now()) {
		sb.AgentOK = nil
		return
	}
	ok := false
	sb.AgentOK = &ok
}

func withinAgentProbeGrace(createdAtRFC3339 string, now time.Time) bool {
	if strings.TrimSpace(createdAtRFC3339) == "" {
		// Unknown age — prefer "checking" over a false negative for agent images.
		return true
	}
	t, err := time.Parse(time.RFC3339, createdAtRFC3339)
	if err != nil {
		return true
	}
	return now.Sub(t) < agentProbeGrace
}

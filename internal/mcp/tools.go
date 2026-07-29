package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/cloudinit"
	"github.com/cxdy/grain/internal/presets"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names exposed by the grain MCP server (stable for hosts and tests).
const (
	ToolHealth         = "grain_health"
	ToolListVMs        = "grain_list_vms"
	ToolGetVM          = "grain_get_vm"
	ToolCreateVM       = "grain_create_vm"
	ToolStartVM        = "grain_start_vm"
	ToolStopVM         = "grain_stop_vm"
	ToolDeleteVM       = "grain_delete_vm"
	ToolExec           = "grain_exec"
	ToolWriteFile      = "grain_write_file"
	ToolReadFile       = "grain_read_file"
	ToolPutTar         = "grain_put_tar"
	ToolGetTar         = "grain_get_tar"
	ToolAgentHealth    = "grain_agent_health"
	ToolLogs           = "grain_logs"
	ToolStats          = "grain_stats"
	ToolWorkspace      = "grain_workspace_sandbox"
	ToolForwardAdd     = "grain_forward_add"
	ToolForwardRemove  = "grain_forward_remove"
	ToolImageList      = "grain_image_list"
	ToolImagePull      = "grain_image_pull"
	ToolFSReadDir      = "grain_fs_readdir"
	ToolFSStat         = "grain_fs_stat"
	ToolFSMkdir        = "grain_fs_mkdir"
	ToolFSRemove       = "grain_fs_remove"
	ToolAct            = "grain_act"
	ToolK3s            = "grain_k3s"
	DefaultCreateImage = "grain-ubuntu"
	DefaultCreateWait  = client.WaitAgent
	DefaultExecTimeout = 15 * time.Minute
)

// ToolNames returns the registered tool identifiers in a stable order.
func ToolNames() []string {
	return []string{
		ToolHealth,
		ToolListVMs,
		ToolGetVM,
		ToolCreateVM,
		ToolStartVM,
		ToolStopVM,
		ToolDeleteVM,
		ToolExec,
		ToolWriteFile,
		ToolReadFile,
		ToolPutTar,
		ToolGetTar,
		ToolAgentHealth,
		ToolLogs,
		ToolStats,
		ToolWorkspace,
		ToolForwardAdd,
		ToolForwardRemove,
		ToolImageList,
		ToolImagePull,
		ToolFSReadDir,
		ToolFSStat,
		ToolFSMkdir,
		ToolFSRemove,
		ToolAct,
		ToolK3s,
	}
}

// Server wraps a grain daemon client and optional host-side helpers.
type Server struct {
	Client *client.Client
	Host   HostOps
}

// ServerOptions configures NewMCPServer.
type ServerOptions struct {
	Version string
	Client  *client.Client
	// DataDir for image list/pull and serial/qemu logs (default ~/.grain).
	DataDir string
	// Host overrides LocalHost (tests).
	Host HostOps
}

// NewMCPServer builds an MCP server that talks to the grain daemon via c.
func NewMCPServer(version string, c *client.Client) *mcp.Server {
	return NewMCPServerOpts(ServerOptions{Version: version, Client: c})
}

// NewMCPServerOpts builds an MCP server with host-side data dir / HostOps.
func NewMCPServerOpts(opts ServerOptions) *mcp.Server {
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	host := opts.Host
	if host == nil {
		host = NewLocalHost(opts.DataDir)
	}
	s := &Server{Client: opts.Client, Host: host}
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "grain",
		Version: version,
	}, nil)
	s.register(srv)
	return srv
}

func (s *Server) register(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolHealth,
		Description: "Check that the grain daemon is healthy and return daemon name/version (GET /healthz + /info).",
	}, s.toolHealth)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolListVMs,
		Description: "List all grain sandboxes/VMs managed by the daemon (GET /vms).",
	}, s.toolListVMs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolGetVM,
		Description: "Get details for one sandbox by name (GET /vms/{name}).",
	}, s.toolGetVM)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolCreateVM,
		Description: "Create a sandbox/VM (POST /vms). Defaults: image=grain-ubuntu, wait=agent (agent-ready). " +
			"Ephemeral unless persistent=true. Prefer grain_workspace_sandbox for repo work.",
	}, s.toolCreateVM)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolStartVM,
		Description: "Start a stopped persistent sandbox (POST /vms/{name}/start).",
	}, s.toolStartVM)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolStopVM,
		Description: "Stop a sandbox (POST /vms/{name}/shutdown). Ephemeral VMs are deleted; persistent VMs stop.",
	}, s.toolStopVM)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolDeleteVM,
		Description: "Delete a sandbox (DELETE /vms/{name}). Idempotent: missing VM returns ok with missing=true.",
	}, s.toolDeleteVM)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolExec,
		Description: "Run a command in a sandbox via the guest agent. Default streams stdout/stderr frames into the " +
			"result (progress-friendly). Prefer focused builds (e.g. go build ./cmd/grain) over full host test suites " +
			"(go test ./...). Use timeout (Go duration) to cap runaway installs. Optional cwd.",
	}, s.toolExec)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolWriteFile,
		Description: "Write a file in the guest (PUT /vms/{name}/cp). Provide text content or base64; optional mode.",
	}, s.toolWriteFile)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolReadFile,
		Description: "Read a guest file (GET /vms/{name}/cp). Returns text or base64 if not valid UTF-8.",
	}, s.toolReadFile)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolPutTar,
		Description: "Upload a base64 tar archive and extract at guest_path (PUT /vms/{name}/cp?format=tar).",
	}, s.toolPutTar)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolGetTar,
		Description: "Download guest_path as a base64 tar archive (GET /vms/{name}/cp?format=tar).",
	}, s.toolGetTar)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolAgentHealth,
		Description: "Guest grain-agent health for a named VM (GET /vms/{name}/agent/health).",
	}, s.toolAgentHealth)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolLogs,
		Description: "Read guest serial console log (default) or QEMU hypervisor log from the host data dir.",
	}, s.toolLogs)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolStats,
		Description: "Guest resource stats via agent (GET /vms/{name}/stats): uptime, mem, load, disk.",
	}, s.toolStats)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolWorkspace,
		Description: "One-shot coding sandbox: create (or reuse) a VM, mount a host workspace at /work (default cwd), " +
			"wait for agent, optionally run first_cmd. Defaults image=grain-ubuntu wait=agent.",
	}, s.toolWorkspace)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolForwardAdd,
		Description: "Add a live host→guest TCP forward on a running VM. host_port 0 allocates. Returns resolved host_port.",
	}, s.toolForwardAdd)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolForwardRemove,
		Description: "Remove a live host forward by host_port.",
	}, s.toolForwardRemove)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolImageList,
		Description: "List base image catalog with local readiness (host data dir). Check grain-ubuntu before create.",
	}, s.toolImageList)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolImagePull,
		Description: "Pull a base image into the host data dir (e.g. grain-ubuntu, ubuntu-cloud).",
	}, s.toolImagePull)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolFSReadDir,
		Description: "List a guest directory via agent (fs/readdir).",
	}, s.toolFSReadDir)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolFSStat,
		Description: "Stat a guest path via agent (fs/stat).",
	}, s.toolFSStat)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolFSMkdir,
		Description: "Create a guest directory via agent (fs/mkdir).",
	}, s.toolFSMkdir)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolFSRemove,
		Description: "Remove a guest path via agent (fs/remove).",
	}, s.toolFSRemove)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolAct,
		Description: "Run GitHub Actions via nektos/act in an ephemeral grain sandbox (act preset: Docker + act). " +
			"Mounts host_dir (default cwd) at /work, waits for agent/docker/act, runs act with act_args. " +
			"Deletes the VM unless keep=true. Requires daemon, QEMU, network.",
	}, s.toolAct)

	mcp.AddTool(srv, &mcp.Tool{
		Name: ToolK3s,
		Description: "Create a k3s lab sandbox (k3s userdata preset, API port 6443, higher memory). " +
			"Waits for agent then optionally waits for kubectl/k3s readiness. kind is not a separate preset—use k3s.",
	}, s.toolK3s)
}

// --- input types ---

type emptyIn struct{}

type nameIn struct {
	Name string `json:"name" jsonschema:"sandbox/VM name"`
}

type createIn struct {
	Name       string   `json:"name,omitempty" jsonschema:"optional sandbox name"`
	Persistent bool     `json:"persistent,omitempty" jsonschema:"keep disk after stop"`
	CPUs       int      `json:"cpus,omitempty" jsonschema:"vCPU count"`
	MemoryMB   int      `json:"memory_mb,omitempty" jsonschema:"memory in MiB"`
	DiskGB     int      `json:"disk_gb,omitempty" jsonschema:"disk size in GiB"`
	Image      string   `json:"image,omitempty" jsonschema:"image id; default grain-ubuntu"`
	Arch       string   `json:"arch,omitempty" jsonschema:"guest arch arm64|amd64"`
	GPU        string   `json:"gpu,omitempty" jsonschema:"virtio or empty"`
	Network    string   `json:"network,omitempty" jsonschema:"slirp or overlay"`
	Wait       string   `json:"wait,omitempty" jsonschema:"auto|ssh|agent|userdata; default agent"`
	Timeout    string   `json:"timeout,omitempty" jsonschema:"create readiness timeout e.g. 3m"`
	Userdata   string   `json:"userdata,omitempty" jsonschema:"cloud-init userdata"`
	Preset     string   `json:"preset,omitempty" jsonschema:"userdata preset docker|k3s|act"`
	Publish    []string `json:"publish,omitempty" jsonschema:"HOST:GUEST or GUEST ports"`
	Mounts     []string `json:"mounts,omitempty" jsonschema:"HOST:GUEST directory shares"`
}

type execIn struct {
	Name    string   `json:"name" jsonschema:"sandbox/VM name"`
	Cmd     string   `json:"cmd" jsonschema:"command argv0"`
	Args    []string `json:"args,omitempty" jsonschema:"command arguments"`
	Cwd     string   `json:"cwd,omitempty" jsonschema:"optional working directory in guest"`
	Timeout string   `json:"timeout,omitempty" jsonschema:"Go duration cap e.g. 10m; default 15m"`
	Stream  *bool    `json:"stream,omitempty" jsonschema:"stream stdout/stderr frames into result (default true)"`
}

type writeFileIn struct {
	Name     string `json:"name" jsonschema:"sandbox/VM name"`
	Path     string `json:"path" jsonschema:"absolute guest path"`
	Content  string `json:"content,omitempty" jsonschema:"file text content"`
	Base64   string `json:"base64,omitempty" jsonschema:"base64-encoded bytes (if set, used instead of content)"`
	Mode     string `json:"mode,omitempty" jsonschema:"file mode e.g. 0644"`
}

type readFileIn struct {
	Name string `json:"name" jsonschema:"sandbox/VM name"`
	Path string `json:"path" jsonschema:"absolute guest path"`
}

type tarIn struct {
	Name   string `json:"name" jsonschema:"sandbox/VM name"`
	Path   string `json:"path" jsonschema:"guest path (directory or file)"`
	Base64 string `json:"base64,omitempty" jsonschema:"base64 tar for put"`
}

type logsIn struct {
	Name     string `json:"name" jsonschema:"sandbox/VM name"`
	QEMU     bool   `json:"qemu,omitempty" jsonschema:"true for QEMU hypervisor log instead of serial"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"max bytes from end of log (default 256KiB)"`
}

type workspaceIn struct {
	Name       string   `json:"name,omitempty" jsonschema:"sandbox name; generated if empty"`
	HostDir    string   `json:"host_dir,omitempty" jsonschema:"host path to mount (default: process cwd)"`
	GuestPath  string   `json:"guest_path,omitempty" jsonschema:"guest mount point (default /work)"`
	Image      string   `json:"image,omitempty" jsonschema:"default grain-ubuntu"`
	Wait       string   `json:"wait,omitempty" jsonschema:"default agent"`
	Timeout    string   `json:"timeout,omitempty" jsonschema:"create timeout e.g. 5m"`
	CPUs       int      `json:"cpus,omitempty"`
	MemoryMB   int      `json:"memory_mb,omitempty"`
	Persistent bool     `json:"persistent,omitempty"`
	Reuse      bool     `json:"reuse,omitempty" jsonschema:"if true and name exists and running, skip create"`
	FirstCmd   string   `json:"first_cmd,omitempty" jsonschema:"optional command after ready"`
	FirstArgs  []string `json:"first_args,omitempty"`
	Publish    []string `json:"publish,omitempty"`
}

type forwardAddIn struct {
	Name      string `json:"name" jsonschema:"sandbox/VM name"`
	HostPort  int    `json:"host_port,omitempty" jsonschema:"host port; 0 = allocate"`
	GuestPort int    `json:"guest_port" jsonschema:"guest TCP port"`
}

type forwardRmIn struct {
	Name     string `json:"name" jsonschema:"sandbox/VM name"`
	HostPort int    `json:"host_port" jsonschema:"host port to remove"`
}

type imagePullIn struct {
	ID string `json:"id" jsonschema:"image id e.g. grain-ubuntu"`
}

type fsPathIn struct {
	Name      string `json:"name" jsonschema:"sandbox/VM name"`
	Path      string `json:"path" jsonschema:"guest path"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"for mkdir/remove"`
	Mode      string `json:"mode,omitempty" jsonschema:"for mkdir e.g. 0755"`
}

type actIn struct {
	HostDir string   `json:"host_dir,omitempty" jsonschema:"project dir (default cwd); mounted at /work"`
	Name    string   `json:"name,omitempty" jsonschema:"sandbox name"`
	Keep    bool     `json:"keep,omitempty" jsonschema:"keep VM after act exits"`
	CPUs    int      `json:"cpus,omitempty"`
	MemoryMB int     `json:"memory_mb,omitempty"`
	Image   string   `json:"image,omitempty"`
	Timeout string   `json:"timeout,omitempty" jsonschema:"overall timeout default 15m"`
	ActArgs []string `json:"act_args,omitempty" jsonschema:"args passed to act (default -l)"`
}

type k3sIn struct {
	Name      string `json:"name,omitempty"`
	HostDir   string `json:"host_dir,omitempty" jsonschema:"optional host dir mounted at /work"`
	Ephemeral bool   `json:"ephemeral,omitempty" jsonschema:"if true, delete-friendly non-persistent disk (default false = persistent lab)"`
	CPUs      int    `json:"cpus,omitempty"`
	MemoryMB  int    `json:"memory_mb,omitempty"`
	Image     string `json:"image,omitempty"`
	Timeout   string `json:"timeout,omitempty"`
	SkipWait  bool   `json:"skip_wait,omitempty" jsonschema:"if true, do not wait for kubectl nodes Ready"`
}

// --- core handlers ---

func (s *Server) toolHealth(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	if err := s.Client.Health(ctx); err != nil {
		return toolErr(fmt.Errorf("daemon health: %w", err))
	}
	info, err := s.Client.Info(ctx)
	if err != nil {
		return toolErr(fmt.Errorf("daemon info: %w", err))
	}
	return toolJSON(map[string]any{"ok": true, "info": info})
}

func (s *Server) toolListVMs(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	list, err := s.Client.List(ctx)
	if err != nil {
		return toolErr(err)
	}
	if list == nil {
		list = []*client.Instance{}
	}
	return toolJSON(map[string]any{"vms": list, "count": len(list)})
}

func (s *Server) toolGetVM(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	inst, err := s.Client.Get(ctx, in.Name)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(inst)
}

func (s *Server) toolCreateVM(ctx context.Context, req *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, any, error) {
	cr, err := s.buildCreate(in)
	if err != nil {
		return toolErr(err)
	}
	inst, err := s.Client.Create(ctx, cr)
	if err != nil {
		return toolErr(err)
	}
	_ = req
	return toolJSON(inst)
}

func (s *Server) buildCreate(in createIn) (client.CreateRequest, error) {
	req := client.CreateRequest{
		Name:       strings.TrimSpace(in.Name),
		Persistent: in.Persistent,
		CPUs:       in.CPUs,
		MemoryMB:   in.MemoryMB,
		DiskGB:     in.DiskGB,
		Image:      strings.TrimSpace(in.Image),
		Arch:       strings.TrimSpace(in.Arch),
		GPU:        strings.TrimSpace(in.GPU),
		Network:    strings.TrimSpace(in.Network),
		Userdata:   in.Userdata,
		Wait:       strings.TrimSpace(in.Wait),
		Timeout:    strings.TrimSpace(in.Timeout),
	}
	if req.Image == "" {
		req.Image = DefaultCreateImage
	}
	if req.Wait == "" {
		req.Wait = DefaultCreateWait
	}
	preset := strings.TrimSpace(in.Preset)
	if preset != "" {
		ud, err := presets.Get(preset)
		if err != nil {
			return req, err
		}
		if req.Userdata == "" {
			req.Userdata = ud
		} else {
			merged, err := cloudinit.MergeUserData(ud, req.Userdata)
			if err != nil {
				return req, err
			}
			req.Userdata = merged
		}
		if dc, dm := presets.DefaultResources(preset); dc > 0 || dm > 0 {
			if req.CPUs <= 0 {
				req.CPUs = dc
			}
			if req.MemoryMB <= 0 {
				req.MemoryMB = dm
			}
		}
		if presets.NeedsAPIServerPort(preset) {
			// ensure 6443 publish
			has := false
			for _, p := range in.Publish {
				if strings.HasSuffix(p, ":6443") || p == "6443" {
					has = true
					break
				}
			}
			if !has {
				in.Publish = append(in.Publish, "6443")
			}
		}
		if req.Tags == nil {
			req.Tags = map[string]string{}
		}
		req.Tags["preset"] = preset
	}
	forwards, err := parsePublish(in.Publish)
	if err != nil {
		return req, err
	}
	req.Forwards = forwards
	mounts, err := parseMounts(in.Mounts)
	if err != nil {
		return req, err
	}
	req.Mounts = mounts
	return req, nil
}

func (s *Server) toolStartVM(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	inst, err := s.Client.Start(ctx, in.Name)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(inst)
}

func (s *Server) toolStopVM(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	if err := s.Client.Stop(ctx, in.Name); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "name": in.Name, "action": "stop"})
}

func (s *Server) toolDeleteVM(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	if err := s.Client.Delete(ctx, in.Name); err != nil {
		if isNotFound(err) {
			return toolJSON(map[string]any{"ok": true, "name": in.Name, "action": "delete", "missing": true})
		}
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "name": in.Name, "action": "delete", "missing": false})
}

func (s *Server) toolExec(ctx context.Context, req *mcp.CallToolRequest, in execIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	if strings.TrimSpace(in.Cmd) == "" {
		return toolErr(fmt.Errorf("cmd is required"))
	}
	timeout := DefaultExecTimeout
	if strings.TrimSpace(in.Timeout) != "" {
		d, err := time.ParseDuration(in.Timeout)
		if err != nil {
			return toolErr(fmt.Errorf("timeout: %w", err))
		}
		if d > 0 {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stream := true
	if in.Stream != nil {
		stream = *in.Stream
	}

	if !stream {
		res, err := s.Client.Exec(ctx, in.Name, in.Cmd, in.Args...)
		if err != nil {
			return toolErr(err)
		}
		return toolJSON(res)
	}

	var stdout, stderr strings.Builder
	var progress []string
	lastNotify := time.Now()
	code, err := s.Client.ExecStream(ctx, in.Name, client.ExecOpts{
		Cmd:  in.Cmd,
		Args: in.Args,
		Cwd:  in.Cwd,
	}, func(f client.ExecFrame) error {
		switch f.Type {
		case "stdout":
			stdout.WriteString(f.Data)
			progress = appendProgress(progress, "stdout", f.Data)
		case "stderr":
			stderr.WriteString(f.Data)
			progress = appendProgress(progress, "stderr", f.Data)
		}
		// Optional MCP progress token for hosts that support it.
		if req != nil && req.Session != nil && req.Params != nil && time.Since(lastNotify) > 500*time.Millisecond {
			if token := req.Params.GetProgressToken(); token != nil {
				_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: token,
					Message:       fmt.Sprintf("stdout=%d stderr=%d bytes", stdout.Len(), stderr.Len()),
					Progress:      float64(stdout.Len() + stderr.Len()),
				})
				lastNotify = time.Now()
			}
		}
		return nil
	})
	if err != nil {
		// Fall back to buffered if stream unsupported.
		res, err2 := s.Client.Exec(ctx, in.Name, in.Cmd, in.Args...)
		if err2 != nil {
			return toolErr(fmt.Errorf("exec stream: %w", err))
		}
		return toolJSON(map[string]any{
			"stdout":   res.Stdout,
			"stderr":   res.Stderr,
			"exit_code": res.ExitCode,
			"streamed": false,
			"note":     "stream failed; returned buffered result",
		})
	}
	out := map[string]any{
		"stdout":    truncateRunes(stdout.String(), 512*1024),
		"stderr":    truncateRunes(stderr.String(), 256*1024),
		"exit_code": code,
		"streamed":  true,
		"progress":  progress,
	}
	// Multi-part content: progress lines first, then JSON summary.
	parts := []mcp.Content{}
	if len(progress) > 0 {
		parts = append(parts, &mcp.TextContent{Text: "=== progress ===\n" + strings.Join(progress, "")})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	parts = append(parts, &mcp.TextContent{Text: string(b)})
	return &mcp.CallToolResult{Content: parts}, out, nil
}

func appendProgress(progress []string, stream, data string) []string {
	if data == "" {
		return progress
	}
	// Cap stored progress chunks so huge builds don't blow memory.
	const maxChunks = 200
	chunk := data
	if len(chunk) > 4096 {
		chunk = chunk[:4096] + "…"
	}
	progress = append(progress, fmt.Sprintf("[%s] %s", stream, chunk))
	if len(progress) > maxChunks {
		progress = progress[len(progress)-maxChunks:]
	}
	return progress
}

func (s *Server) toolWriteFile(ctx context.Context, _ *mcp.CallToolRequest, in writeFileIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" {
		return toolErr(fmt.Errorf("name and path are required"))
	}
	var data []byte
	var err error
	if strings.TrimSpace(in.Base64) != "" {
		data, err = base64.StdEncoding.DecodeString(in.Base64)
		if err != nil {
			return toolErr(fmt.Errorf("base64: %w", err))
		}
	} else {
		data = []byte(in.Content)
	}
	opts := client.CPOpts{Mode: strings.TrimSpace(in.Mode)}
	if err := s.Client.PutFile(ctx, in.Name, in.Path, bytes.NewReader(data), int64(len(data)), opts); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "name": in.Name, "path": in.Path, "bytes": len(data)})
}

func (s *Server) toolReadFile(ctx context.Context, _ *mcp.CallToolRequest, in readFileIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" {
		return toolErr(fmt.Errorf("name and path are required"))
	}
	var buf bytes.Buffer
	if err := s.Client.GetFile(ctx, in.Name, in.Path, &buf); err != nil {
		return toolErr(err)
	}
	b := buf.Bytes()
	if isMostlyText(b) {
		return toolJSON(map[string]any{"path": in.Path, "content": string(b), "bytes": len(b)})
	}
	return toolJSON(map[string]any{
		"path":   in.Path,
		"base64": base64.StdEncoding.EncodeToString(b),
		"bytes":  len(b),
		"binary": true,
	})
}

func (s *Server) toolPutTar(ctx context.Context, _ *mcp.CallToolRequest, in tarIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" || strings.TrimSpace(in.Base64) == "" {
		return toolErr(fmt.Errorf("name, path, and base64 are required"))
	}
	raw, err := base64.StdEncoding.DecodeString(in.Base64)
	if err != nil {
		return toolErr(fmt.Errorf("base64: %w", err))
	}
	if err := s.Client.PutTar(ctx, in.Name, in.Path, bytes.NewReader(raw)); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "path": in.Path, "bytes": len(raw)})
}

func (s *Server) toolGetTar(ctx context.Context, _ *mcp.CallToolRequest, in tarIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" {
		return toolErr(fmt.Errorf("name and path are required"))
	}
	var buf bytes.Buffer
	if err := s.Client.GetTar(ctx, in.Name, in.Path, &buf); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{
		"path":   in.Path,
		"base64": base64.StdEncoding.EncodeToString(buf.Bytes()),
		"bytes":  buf.Len(),
	})
}

func (s *Server) toolAgentHealth(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	h, err := s.Client.AgentHealth(ctx, in.Name)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(h)
}

func (s *Server) toolLogs(ctx context.Context, _ *mcp.CallToolRequest, in logsIn) (*mcp.CallToolResult, any, error) {
	_ = ctx
	if s.Host == nil {
		return toolErr(fmt.Errorf("host log access not configured"))
	}
	path, content, err := s.Host.ReadVMLog(in.Name, in.QEMU, in.MaxBytes)
	if err != nil {
		return toolErr(err)
	}
	kind := "serial"
	if in.QEMU {
		kind = "qemu"
	}
	return toolJSON(map[string]any{"name": in.Name, "kind": kind, "path": path, "content": content})
}

func (s *Server) toolStats(ctx context.Context, _ *mcp.CallToolRequest, in nameIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	st, err := s.Client.Stats(ctx, in.Name)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(st)
}

func (s *Server) toolWorkspace(ctx context.Context, req *mcp.CallToolRequest, in workspaceIn) (*mcp.CallToolResult, any, error) {
	hostDir := strings.TrimSpace(in.HostDir)
	if hostDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return toolErr(err)
		}
		hostDir = wd
	}
	hostDir, err := filepath.Abs(hostDir)
	if err != nil {
		return toolErr(err)
	}
	guest := strings.TrimSpace(in.GuestPath)
	if guest == "" {
		guest = "/work"
	}
	name := strings.TrimSpace(in.Name)

	if in.Reuse && name != "" {
		if inst, err := s.Client.Get(ctx, name); err == nil && inst.Status == client.StatusRunning {
			out := map[string]any{"reused": true, "instance": inst, "host_dir": hostDir, "guest_path": guest}
			if strings.TrimSpace(in.FirstCmd) != "" {
				res, err := s.runFirst(ctx, req, name, in.FirstCmd, in.FirstArgs)
				if err != nil {
					return toolErr(err)
				}
				out["first_command"] = res
			}
			return toolJSON(out)
		}
	}

	cr, err := s.buildCreate(createIn{
		Name:       name,
		Persistent: in.Persistent,
		CPUs:       in.CPUs,
		MemoryMB:   in.MemoryMB,
		Image:      in.Image,
		Wait:       in.Wait,
		Timeout:    in.Timeout,
		Publish:    in.Publish,
		Mounts:     []string{hostDir + ":" + guest},
	})
	if err != nil {
		return toolErr(err)
	}
	inst, err := s.Client.Create(ctx, cr)
	if err != nil {
		return toolErr(err)
	}
	out := map[string]any{"reused": false, "instance": inst, "host_dir": hostDir, "guest_path": guest}
	if strings.TrimSpace(in.FirstCmd) != "" {
		res, err := s.runFirst(ctx, req, inst.Name, in.FirstCmd, in.FirstArgs)
		if err != nil {
			out["first_command_error"] = err.Error()
			return toolJSON(out)
		}
		out["first_command"] = res
	}
	return toolJSON(out)
}

func (s *Server) runFirst(ctx context.Context, req *mcp.CallToolRequest, name, cmd string, args []string) (any, error) {
	stream := true
	res, _, err := s.toolExec(ctx, req, execIn{Name: name, Cmd: cmd, Args: args, Stream: &stream})
	if err != nil {
		return nil, err
	}
	if res != nil && len(res.Content) > 0 {
		if t, ok := res.Content[len(res.Content)-1].(*mcp.TextContent); ok {
			var v any
			if json.Unmarshal([]byte(t.Text), &v) == nil {
				return v, nil
			}
			return t.Text, nil
		}
	}
	return map[string]any{"ok": true}, nil
}

func (s *Server) toolForwardAdd(ctx context.Context, _ *mcp.CallToolRequest, in forwardAddIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || in.GuestPort <= 0 {
		return toolErr(fmt.Errorf("name and guest_port are required"))
	}
	lf, err := s.Client.AddForward(ctx, in.Name, in.HostPort, in.GuestPort)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{
		"ok":         true,
		"name":       in.Name,
		"host_port":  lf.HostPort,
		"guest_port": lf.GuestPort,
		"localhost":  fmt.Sprintf("127.0.0.1:%d", lf.HostPort),
	})
}

func (s *Server) toolForwardRemove(ctx context.Context, _ *mcp.CallToolRequest, in forwardRmIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || in.HostPort <= 0 {
		return toolErr(fmt.Errorf("name and host_port are required"))
	}
	if err := s.Client.RemoveForward(ctx, in.Name, in.HostPort); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "name": in.Name, "host_port": in.HostPort})
}

func (s *Server) toolImageList(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	_ = ctx
	if s.Host == nil {
		return toolErr(fmt.Errorf("host image access not configured"))
	}
	list, err := s.Host.ImageList()
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"images": list, "data_dir": s.Host.DataDir()})
}

func (s *Server) toolImagePull(ctx context.Context, _ *mcp.CallToolRequest, in imagePullIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.ID) == "" {
		return toolErr(fmt.Errorf("id is required"))
	}
	if s.Host == nil {
		return toolErr(fmt.Errorf("host image access not configured"))
	}
	if err := s.Host.ImagePull(ctx, in.ID); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "id": in.ID})
}

func (s *Server) toolFSReadDir(ctx context.Context, _ *mcp.CallToolRequest, in fsPathIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" {
		return toolErr(fmt.Errorf("name and path are required"))
	}
	ents, err := s.Client.ReadDir(ctx, in.Name, in.Path)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"path": in.Path, "entries": ents})
}

func (s *Server) toolFSStat(ctx context.Context, _ *mcp.CallToolRequest, in fsPathIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" {
		return toolErr(fmt.Errorf("name and path are required"))
	}
	st, err := s.Client.Stat(ctx, in.Name, in.Path)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(st)
}

func (s *Server) toolFSMkdir(ctx context.Context, _ *mcp.CallToolRequest, in fsPathIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" {
		return toolErr(fmt.Errorf("name and path are required"))
	}
	if err := s.Client.Mkdir(ctx, in.Name, in.Path, in.Recursive, in.Mode); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "path": in.Path})
}

func (s *Server) toolFSRemove(ctx context.Context, _ *mcp.CallToolRequest, in fsPathIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Path) == "" {
		return toolErr(fmt.Errorf("name and path are required"))
	}
	if err := s.Client.Remove(ctx, in.Name, in.Path, in.Recursive); err != nil {
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "path": in.Path})
}

func (s *Server) toolAct(ctx context.Context, req *mcp.CallToolRequest, in actIn) (*mcp.CallToolResult, any, error) {
	hostDir := strings.TrimSpace(in.HostDir)
	if hostDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return toolErr(err)
		}
		hostDir = wd
	}
	hostDir, err := filepath.Abs(hostDir)
	if err != nil {
		return toolErr(err)
	}
	timeout := 15 * time.Minute
	if strings.TrimSpace(in.Timeout) != "" {
		d, err := time.ParseDuration(in.Timeout)
		if err != nil {
			return toolErr(fmt.Errorf("timeout: %w", err))
		}
		if d > 0 {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cpus, mem := in.CPUs, in.MemoryMB
	if dc, dm := presets.DefaultResources("act"); true {
		if cpus <= 0 {
			cpus = dc
			if cpus <= 0 {
				cpus = 2
			}
		}
		if mem <= 0 {
			mem = dm
			if mem <= 0 {
				mem = 4096
			}
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "act-" + sanitizeName(filepath.Base(hostDir))
	}
	ud, err := presets.Get("act")
	if err != nil {
		return toolErr(err)
	}
	cr, err := s.buildCreate(createIn{
		Name:     name,
		CPUs:     cpus,
		MemoryMB: mem,
		Image:    in.Image,
		Wait:     client.WaitAgent,
		Timeout:  timeout.String(),
		Userdata: ud,
		Preset:   "", // userdata already set
		Mounts:   []string{hostDir + ":/work"},
	})
	if err != nil {
		return toolErr(err)
	}
	if cr.Tags == nil {
		cr.Tags = map[string]string{}
	}
	cr.Tags["preset"] = "act"
	inst, err := s.Client.Create(ctx, cr)
	if err != nil {
		return toolErr(fmt.Errorf("create act sandbox: %w", err))
	}
	vmName := inst.Name
	if !in.Keep {
		defer func() {
			dctx, c := context.WithTimeout(context.Background(), 2*time.Minute)
			defer c()
			_ = s.Client.Delete(dctx, vmName)
		}()
	}
	// Wait for docker/act roughly: agent health then short exec.
	if _, err := s.Client.AgentHealth(ctx, vmName); err != nil {
		return toolErr(fmt.Errorf("agent health: %w", err))
	}
	// Best-effort wait for act binary (cloud-init may still be installing).
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res, err := s.Client.Exec(ctx, vmName, "bash", "-lc", "command -v act && command -v docker")
		if err == nil && res.ExitCode == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return toolErr(ctx.Err())
		case <-time.After(5 * time.Second):
		}
	}
	actArgs := in.ActArgs
	if len(actArgs) == 0 {
		actArgs = []string{"-l"}
	}
	shellCmd := `export HOME="${HOME:-/home/ubuntu}" PATH="/usr/local/bin:/usr/bin:/bin:$PATH"; ` +
		`mkdir -p "$HOME/.config/act"; ` +
		`if [ ! -s "$HOME/.config/act/actrc" ]; then printf '%s\n' '-P ubuntu-latest=catthehacker/ubuntu:act-latest' '-P ubuntu-24.04=catthehacker/ubuntu:act-latest' '-P ubuntu-22.04=catthehacker/ubuntu:act-22.04' '-P ubuntu-20.04=catthehacker/ubuntu:act-20.04' > "$HOME/.config/act/actrc"; fi; ` +
		`cd /work && act ` + shellJoin(actArgs)
	stream := true
	execRes, out, err := s.toolExec(ctx, req, execIn{
		Name:    vmName,
		Cmd:     "bash",
		Args:    []string{"-lc", shellCmd},
		Stream:  &stream,
		Timeout: time.Until(deadline).String(),
	})
	if err != nil {
		return toolErr(err)
	}
	_ = execRes
	return toolJSON(map[string]any{
		"ok":       true,
		"vm":       vmName,
		"kept":     in.Keep,
		"host_dir": hostDir,
		"act_args": actArgs,
		"result":   out,
	})
}

func (s *Server) toolK3s(ctx context.Context, _ *mcp.CallToolRequest, in k3sIn) (*mcp.CallToolResult, any, error) {
	timeout := 10 * time.Minute
	if strings.TrimSpace(in.Timeout) != "" {
		d, err := time.ParseDuration(in.Timeout)
		if err != nil {
			return toolErr(fmt.Errorf("timeout: %w", err))
		}
		if d > 0 {
			timeout = d
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cpus, mem := in.CPUs, in.MemoryMB
	if dc, dm := presets.DefaultResources("k3s"); true {
		if cpus <= 0 {
			cpus = dc
		}
		if mem <= 0 {
			mem = dm
		}
	}
	// k3s labs are persistent by default; set ephemeral=true for disposable clusters.
	persistent := true
	if in.Ephemeral {
		persistent = false
	}

	var mounts []string
	if d := strings.TrimSpace(in.HostDir); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			return toolErr(err)
		}
		mounts = []string{abs + ":/work"}
	}
	cr, err := s.buildCreate(createIn{
		Name:       strings.TrimSpace(in.Name),
		Persistent: persistent,
		CPUs:       cpus,
		MemoryMB:   mem,
		Image:      in.Image,
		Wait:       client.WaitAgent,
		Timeout:    timeout.String(),
		Preset:     "k3s",
		Mounts:     mounts,
	})
	if err != nil {
		return toolErr(err)
	}
	inst, err := s.Client.Create(ctx, cr)
	if err != nil {
		return toolErr(err)
	}
	out := map[string]any{"instance": inst, "preset": "k3s", "note": "kind is not a separate grain preset; use k3s"}
	if !in.SkipWait {
		deadline := time.Now().Add(timeout)
		var last string
		for time.Now().Before(deadline) {
			res, err := s.Client.Exec(ctx, inst.Name, "bash", "-lc",
				`export KUBECONFIG=/etc/rancher/k3s/k3s.yaml; command -v kubectl >/dev/null && kubectl get nodes 2>/dev/null`)
			if err == nil && res.ExitCode == 0 && strings.Contains(res.Stdout, "Ready") {
				out["k3s_ready"] = true
				out["kubectl_get_nodes"] = res.Stdout
				break
			}
			if res != nil {
				last = res.Stdout + res.Stderr
			}
			select {
			case <-ctx.Done():
				out["k3s_ready"] = false
				out["wait_error"] = ctx.Err().Error()
				return toolJSON(out)
			case <-time.After(5 * time.Second):
			}
		}
		if out["k3s_ready"] != true {
			out["k3s_ready"] = false
			out["wait_note"] = "timeout waiting for nodes Ready"
			out["last"] = last
		}
	}
	return toolJSON(out)
}

// --- helpers ---

func toolJSON(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, v, nil
}

func toolErr(err error) (*mcp.CallToolResult, any, error) {
	if err == nil {
		return toolJSON(map[string]any{"ok": true})
	}
	return nil, nil, err
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "404") || strings.Contains(s, "no such")
}

func isMostlyText(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	// Reject if contains NUL
	if bytes.IndexByte(b, 0) >= 0 {
		return false
	}
	return true
}

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n…(truncated)"
}

func sanitizeName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "sandbox"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func shellJoin(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = strconv.Quote(a)
	}
	return strings.Join(parts, " ")
}

// parsePublish accepts "HOST:GUEST", ":GUEST", or "GUEST".
func parsePublish(items []string) ([]client.PortForward, error) {
	var out []client.PortForward
	for _, raw := range items {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		host, guest, err := splitHostGuestPort(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, client.PortForward{HostPort: host, GuestPort: guest, Proto: "tcp"})
	}
	return out, nil
}

func splitHostGuestPort(s string) (host, guest int, err error) {
	if !strings.Contains(s, ":") {
		g, err := strconv.Atoi(s)
		if err != nil || g <= 0 {
			return 0, 0, fmt.Errorf("invalid publish %q (want HOST:GUEST or GUEST)", s)
		}
		return 0, g, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid publish %q (want HOST:GUEST)", s)
	}
	if parts[0] != "" {
		host, err = strconv.Atoi(parts[0])
		if err != nil || host < 0 {
			return 0, 0, fmt.Errorf("invalid host port in %q", s)
		}
	}
	guest, err = strconv.Atoi(parts[1])
	if err != nil || guest <= 0 {
		return 0, 0, fmt.Errorf("invalid guest port in %q", s)
	}
	return host, guest, nil
}

func parseMounts(items []string) ([]client.Mount, error) {
	var out []client.Mount
	for _, raw := range items {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		i := strings.LastIndex(raw, ":")
		if i <= 0 || i == len(raw)-1 {
			return nil, fmt.Errorf("invalid mount %q (want HOST:GUEST)", raw)
		}
		host, guest := raw[:i], raw[i+1:]
		if host == "" || guest == "" {
			return nil, fmt.Errorf("invalid mount %q (want HOST:GUEST)", raw)
		}
		out = append(out, client.Mount{Host: host, Guest: guest})
	}
	return out, nil
}

// Ensure unused imports compile when tools grow.
var _ = io.EOF

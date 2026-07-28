package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cxdy/grain/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names exposed by the grain MCP server (stable for hosts and tests).
const (
	ToolHealth   = "grain_health"
	ToolListVMs  = "grain_list_vms"
	ToolGetVM    = "grain_get_vm"
	ToolCreateVM = "grain_create_vm"
	ToolStartVM  = "grain_start_vm"
	ToolStopVM   = "grain_stop_vm"
	ToolDeleteVM = "grain_delete_vm"
	ToolExec     = "grain_exec"
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
	}
}

// Server wraps a grain daemon client and registers MCP tools.
type Server struct {
	Client *client.Client
}

// NewMCPServer builds an MCP server that talks to the grain daemon via c.
// version is reported in the MCP implementation metadata.
func NewMCPServer(version string, c *client.Client) *mcp.Server {
	if version == "" {
		version = "dev"
	}
	s := &Server{Client: c}
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
		Name:        ToolCreateVM,
		Description: "Create a sandbox/VM (POST /vms). Ephemeral by default. Waits for readiness when wait is set (auto|ssh|agent|userdata).",
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
		Description: "Delete a sandbox and its resources (DELETE /vms/{name}).",
	}, s.toolDeleteVM)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        ToolExec,
		Description: "Run a command inside a sandbox via the guest agent (POST /vms/{name}/exec, buffered).",
	}, s.toolExec)
}

// --- input types (jsonschema tags drive MCP tool schemas) ---

type emptyIn struct{}

type nameIn struct {
	Name string `json:"name" jsonschema:"sandbox/VM name"`
}

type createIn struct {
	Name       string   `json:"name,omitempty" jsonschema:"optional sandbox name (daemon generates one if empty)"`
	Persistent bool     `json:"persistent,omitempty" jsonschema:"keep disk after stop (default false = ephemeral)"`
	CPUs       int      `json:"cpus,omitempty" jsonschema:"vCPU count"`
	MemoryMB   int      `json:"memory_mb,omitempty" jsonschema:"memory in MiB"`
	DiskGB     int      `json:"disk_gb,omitempty" jsonschema:"disk size in GiB"`
	Image      string   `json:"image,omitempty" jsonschema:"image id (auto, grain-ubuntu, ubuntu-cloud, alpine-cloud, …)"`
	Arch       string   `json:"arch,omitempty" jsonschema:"guest arch arm64|amd64"`
	GPU        string   `json:"gpu,omitempty" jsonschema:"virtio for virtio-gpu, or empty"`
	Network    string   `json:"network,omitempty" jsonschema:"slirp (default) or overlay"`
	Wait       string   `json:"wait,omitempty" jsonschema:"readiness: auto|ssh|agent|userdata"`
	Timeout    string   `json:"timeout,omitempty" jsonschema:"create readiness timeout Go duration e.g. 3m"`
	Userdata   string   `json:"userdata,omitempty" jsonschema:"cloud-init userdata string"`
	Publish    []string `json:"publish,omitempty" jsonschema:"port publishes HOST:GUEST or GUEST (e.g. 8080:80)"`
	Mounts     []string `json:"mounts,omitempty" jsonschema:"host dir shares HOST:GUEST (e.g. /tmp/work:/work)"`
}

type execIn struct {
	Name string   `json:"name" jsonschema:"sandbox/VM name"`
	Cmd  string   `json:"cmd" jsonschema:"command to run (argv0)"`
	Args []string `json:"args,omitempty" jsonschema:"command arguments"`
}

func (s *Server) toolHealth(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, any, error) {
	if err := s.Client.Health(ctx); err != nil {
		return toolErr(fmt.Errorf("daemon health: %w", err))
	}
	info, err := s.Client.Info(ctx)
	if err != nil {
		return toolErr(fmt.Errorf("daemon info: %w", err))
	}
	return toolJSON(map[string]any{
		"ok":   true,
		"info": info,
	})
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

func (s *Server) toolCreateVM(ctx context.Context, _ *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, any, error) {
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
	forwards, err := parsePublish(in.Publish)
	if err != nil {
		return toolErr(err)
	}
	req.Forwards = forwards
	mounts, err := parseMounts(in.Mounts)
	if err != nil {
		return toolErr(err)
	}
	req.Mounts = mounts

	inst, err := s.Client.Create(ctx, req)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(inst)
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
		return toolErr(err)
	}
	return toolJSON(map[string]any{"ok": true, "name": in.Name, "action": "delete"})
}

func (s *Server) toolExec(ctx context.Context, _ *mcp.CallToolRequest, in execIn) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(in.Name) == "" {
		return toolErr(fmt.Errorf("name is required"))
	}
	if strings.TrimSpace(in.Cmd) == "" {
		return toolErr(fmt.Errorf("cmd is required"))
	}
	res, err := s.Client.Exec(ctx, in.Name, in.Cmd, in.Args...)
	if err != nil {
		return toolErr(err)
	}
	return toolJSON(res)
}

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
	// Returning a Go error fails the tool call at the protocol layer with a readable message.
	return nil, nil, err
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
	// GUEST only
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
		// Split on last colon so Windows-style paths are not required; host:guest only.
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

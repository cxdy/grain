package client

import "time"

// Status of a sandbox VM.
type Status string

const (
	StatusCreating Status = "creating"
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusError    Status = "error"
)

// PortForward maps a host port to a guest port (SLIRP hostfwd).
// HostPort 0 means allocate a free high port at start time.
type PortForward struct {
	HostPort  int    `json:"host_port"`
	GuestPort int    `json:"guest_port"`
	Proto     string `json:"proto,omitempty"`
}

// Mount shares a host directory into the guest via virtio-9p.
type Mount struct {
	Host  string `json:"host"`
	Guest string `json:"guest"`
	Tag   string `json:"tag,omitempty"`
}

// Instance is a managed microVM as returned by the daemon API.
type Instance struct {
	Name       string            `json:"name"`
	Status     Status            `json:"status"`
	Persistent bool              `json:"persistent"`
	CPUs       int               `json:"cpus"`
	MemoryMB   int               `json:"memory_mb"`
	DiskGB     int               `json:"disk_gb"`
	Image      string            `json:"image"`
	IP         string            `json:"ip,omitempty"`
	SSHPort    int               `json:"ssh_port,omitempty"`
	AgentPort  int               `json:"agent_port,omitempty"`
	Forwards   []PortForward     `json:"forwards,omitempty"`
	Mounts     []Mount           `json:"mounts,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	Error      string            `json:"error,omitempty"`
	DiskPath   string            `json:"disk_path,omitempty"`
	PID        int               `json:"pid,omitempty"`
}

// CreateRequest is the JSON body for POST /vms.
type CreateRequest struct {
	Name       string            `json:"name,omitempty"`
	Persistent bool              `json:"persistent"`
	CPUs       int               `json:"cpus,omitempty"`
	MemoryMB   int               `json:"memory_mb,omitempty"`
	DiskGB     int               `json:"disk_gb,omitempty"`
	Image      string            `json:"image,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Userdata   string            `json:"userdata,omitempty"`
	Forwards   []PortForward     `json:"forwards,omitempty"`
	Mounts     []Mount           `json:"mounts,omitempty"`
}

// CreateEvent is one NDJSON progress line during streamed create.
type CreateEvent struct {
	Phase    string    `json:"phase"`
	Message  string    `json:"message,omitempty"`
	Name     string    `json:"name,omitempty"`
	Error    string    `json:"error,omitempty"`
	SSHPort  int       `json:"ssh_port,omitempty"`
	Instance *Instance `json:"instance,omitempty"`
}

// Create phase constants (match daemon).
const (
	PhaseImage     = "image"
	PhaseDisk      = "disk"
	PhaseSeed      = "seed"
	PhaseQEMU      = "qemu"
	PhaseWaitSSH   = "wait_ssh"
	PhaseWaitAgent = "wait_agent"
	PhaseReady     = "ready"
	PhaseError     = "error"
)

// Health is guest grain-agent GET /health (proxied via daemon).
type Health struct {
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
	AgentUptime  int64  `json:"agent_uptime_sec"`
	UserdataRan  bool   `json:"userdata_ran"`
}

// ExecResult is a buffered POST /vms/{name}/exec response.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// ExecFrame is one NDJSON line for streaming exec (buffered=false).
type ExecFrame struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp,omitempty"`
	PID       int    `json:"pid,omitempty"`
	Data      string `json:"data,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

// ExecOpts holds optional parameters for buffered or streaming exec.
type ExecOpts struct {
	Cmd  string
	Args []string
	UID  *uint32
	GID  *uint32
	Cwd  string
}

// CPOpts holds optional ownership and mode for PUT /vms/{name}/cp (binary).
type CPOpts struct {
	UID  *uint32
	GID  *uint32
	Mode string // e.g. "0644"
}

// FSInfo describes a guest filesystem entry.
type FSInfo struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // file|directory|symlink
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
	Mode  string `json:"mode"`
}

// MkdirRequest is the JSON body for POST /vms/{name}/fs/mkdir.
type MkdirRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Mode      string `json:"mode"`
}

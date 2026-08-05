package client

import "time"

// Status of a sandbox VM.
type Status string

const (
	StatusCreating  Status = "creating"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusSuspended Status = "suspended"
	StatusStopped   Status = "stopped"
	StatusError     Status = "error"
)

// PortForward maps a host port to a guest port (SLIRP hostfwd).
// HostPort 0 means allocate a free high port at start time.
type PortForward struct {
	HostPort  int    `json:"host_port"`
	GuestPort int    `json:"guest_port"`
	Proto     string `json:"proto,omitempty"`
}

// LiveForward is a runtime SSH local port forward (ssh -N -L) on a running VM.
// Distinct from PortForward (SLIRP hostfwd at create/start).
type LiveForward struct {
	HostPort  int `json:"host_port"`
	GuestPort int `json:"guest_port"`
	PID       int `json:"pid,omitempty"`
}

// Mount shares a host directory into the guest via virtio-9p.
type Mount struct {
	Host  string `json:"host"`
	Guest string `json:"guest"`
	Tag   string `json:"tag,omitempty"`
}

// SocketForward maps a host Unix socket to a guest Unix socket via SSH streamlocal.
type SocketForward struct {
	HostPath  string `json:"host_path"`
	GuestPath string `json:"guest_path"`
	PID       int    `json:"pid,omitempty"`
}

// Instance is a managed microVM as returned by the daemon API.
type Instance struct {
	Name       string `json:"name"`
	Status     Status `json:"status"`
	Persistent bool   `json:"persistent"`
	CPUs       int    `json:"cpus"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	Image      string `json:"image"`
	IP         string `json:"ip,omitempty"`
	SSHPort    int    `json:"ssh_port,omitempty"`
	AgentPort  int    `json:"agent_port,omitempty"`
	// AgentCID is the guest virtio-vsock context ID (0 = TCP hostfwd only).
	AgentCID int           `json:"agent_cid,omitempty"`
	Forwards []PortForward `json:"forwards,omitempty"`
	// LiveForwards are SSH -L tunnels added while running (cleared on stop).
	LiveForwards   []LiveForward     `json:"live_forwards,omitempty"`
	Mounts         []Mount           `json:"mounts,omitempty"`
	SocketForwards []SocketForward   `json:"socket_forwards,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	SuspendedAt    time.Time         `json:"suspended_at,omitempty"`
	Error          string            `json:"error,omitempty"`
	DiskPath       string            `json:"disk_path,omitempty"`
	PID            int               `json:"pid,omitempty"`
}

// Create wait mode values for CreateRequest.Wait (query param, not JSON body).
const (
	WaitAuto      = "auto"      // daemon picks (agent for golden images, else ssh)
	WaitSSH       = "ssh"       // ready when SSH accepts connections
	WaitAgent     = "agent"     // ready when grain-agent /health succeeds
	WaitUserdata  = "userdata"  // ready when agent reports userdata finished
	WaitBootstrap = "bootstrap" // ready when guest readiness protocol reports state=ready
)

// CreateRequest is the JSON body for POST /vms.
// Wait and Timeout are sent as query parameters (not JSON body).
type CreateRequest struct {
	Name           string            `json:"name,omitempty"`
	Persistent     bool              `json:"persistent"`
	CPUs           int               `json:"cpus,omitempty"`
	MemoryMB       int               `json:"memory_mb,omitempty"`
	DiskGB         int               `json:"disk_gb,omitempty"`
	Image          string            `json:"image,omitempty"`
	Arch           string            `json:"arch,omitempty"`    // arm64|amd64; empty=host
	GPU            string            `json:"gpu,omitempty"`     // ""|virtio
	Network        string            `json:"network,omitempty"` // slirp|overlay
	Tags           map[string]string `json:"tags,omitempty"`
	Userdata       string            `json:"userdata,omitempty"`
	Forwards       []PortForward     `json:"forwards,omitempty"`
	Mounts         []Mount           `json:"mounts,omitempty"`
	SocketForwards []SocketForward   `json:"socket_forwards,omitempty"`
	// Wait is auto|ssh|agent|userdata|bootstrap (empty = daemon default/auto).
	// Legacy true/1 maps to ssh on the server.
	Wait string `json:"-"`
	// Timeout is an optional Go duration string for create readiness (e.g. "3m").
	Timeout string `json:"-"`
}

// Stats is guest resource basics from GET /vms/{name}/stats.
type Stats struct {
	UptimeSec float64 `json:"uptime_sec"`
	MemTotal  uint64  `json:"mem_total_bytes"`
	MemAvail  uint64  `json:"mem_available_bytes"`
	Load1     float64 `json:"load1"`
	DiskTotal uint64  `json:"disk_total_bytes,omitempty"`
	DiskFree  uint64  `json:"disk_free_bytes,omitempty"`
}

// SecretMeta is host secret metadata (no payload).
type SecretMeta struct {
	Name      string    `json:"name"`
	Mode      string    `json:"mode,omitempty"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SecretPut is the body for POST /secrets.
type SecretPut struct {
	Name       string  `json:"name"`
	DataBase64 string  `json:"data_base64"`
	Mode       string  `json:"mode,omitempty"`
	UID        *uint32 `json:"uid,omitempty"`
	GID        *uint32 `json:"gid,omitempty"`
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
	PhaseUserdata  = "userdata"
	PhaseBootstrap = "bootstrap"
	PhaseReady     = "ready"
	PhaseError     = "error"
)

// Readiness is the guest bootstrap/readiness contract (proxied via agent health).
type Readiness struct {
	State     string `json:"state,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Message   string `json:"message,omitempty"`
	ReadyName string `json:"ready_name,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Health is guest grain-agent GET /health (proxied via daemon).
type Health struct {
	Hostname     string     `json:"hostname"`
	AgentVersion string     `json:"agent_version"`
	AgentUptime  int64      `json:"agent_uptime_sec"`
	UserdataRan  bool       `json:"userdata_ran"`
	Readiness    *Readiness `json:"readiness,omitempty"`
}

// AgentDeployResult is the JSON body for POST /vms/{name}/agent/deploy.
type AgentDeployResult struct {
	Name   string  `json:"name"`
	Binary string  `json:"binary"`
	Health *Health `json:"health,omitempty"`
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

package vm

import (
	"time"
)

// Status of a sandbox.
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
	HostPort  int    `json:"host_port"` // 0 = allocate free
	GuestPort int    `json:"guest_port"`
	Proto     string `json:"proto,omitempty"` // default tcp
}

// LiveForward is a runtime SSH local port forward for a running VM
// (ssh -N -L hostPort:127.0.0.1:guestPort). Not a SLIRP hostfwd.
type LiveForward struct {
	HostPort  int `json:"host_port"`
	GuestPort int `json:"guest_port"`
	PID       int `json:"pid,omitempty"` // ssh -N process
}

// SocketForward maps a host Unix socket path to a guest Unix socket path
// via OpenSSH streamlocal local forward (ssh -N -L hostPath:guestPath).
// Used for docker-style workflows (e.g. host docker.sock ← guest docker.sock).
type SocketForward struct {
	HostPath  string `json:"host_path"`
	GuestPath string `json:"guest_path"`
	PID       int    `json:"pid,omitempty"` // ssh -N process when running
}

// Mount shares a host directory into the guest via virtio-9p.
// Tag is the 9p mount_tag (auto grain0, grain1, … when empty).
type Mount struct {
	Host  string `json:"host"`
	Guest string `json:"guest"`
	Tag   string `json:"tag,omitempty"`
}

// Instance is a managed microVM (sandbox or long-lived).
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
	// AgentPort is host TCP port forwarded to guest grain-agent (:7475).
	AgentPort int `json:"agent_port,omitempty"`
	// AgentCID is the guest virtio-vsock context ID when vsock transport is
	// enabled (0 = use TCP hostfwd only). Host dials CID:7475 when > 0.
	AgentCID int `json:"agent_cid,omitempty"`
	// Forwards are extra hostfwd entries (beyond SSH :22). Host ports with 0
	// are allocated before start and persisted so restarts reuse them.
	Forwards []PortForward `json:"forwards,omitempty"`
	// LiveForwards are SSH -L forwards managed while the VM is running.
	// Cleared on stop/delete; not re-applied automatically on start.
	LiveForwards []LiveForward `json:"live_forwards,omitempty"`
	// SocketForwards are SSH streamlocal forwards (host unix → guest unix).
	// Config is persisted; PIDs are cleared on stop and re-applied on start.
	SocketForwards []SocketForward `json:"socket_forwards,omitempty"`
	// Mounts are host directories shared into the guest via virtio-9p.
	Mounts []Mount `json:"mounts,omitempty"`
	// Arch is the guest ISA: arm64 | amd64 (empty = host arch).
	// On Apple Silicon, amd64 runs under QEMU TCG (not Apple Rosetta).
	Arch string `json:"arch,omitempty"`
	// GPU selects guest display: "" (none/headless) or "virtio".
	GPU string `json:"gpu,omitempty"`
	// Network: ""|"slirp" (default isolation) or "overlay" (shared L2 between VMs).
	Network   string            `json:"network,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	// SuspendedAt is set when the VM is suspended (process stopped; disk retained).
	SuspendedAt time.Time `json:"suspended_at,omitempty"`
	Error       string    `json:"error,omitempty"`
	// DiskPath is host path to the root disk image.
	DiskPath string `json:"disk_path,omitempty"`
	// PID of hypervisor process when running.
	PID int `json:"pid,omitempty"`
	// QMPPath is the QEMU monitor unix socket (vmDir/qmp.sock when using QEMU).
	QMPPath string `json:"qmp_path,omitempty"`
	// LoadVM is a transient QEMU -loadvm snapshot tag applied on the next Start
	// (not persisted to meta.json).
	LoadVM string `json:"-"`
}

// Wait mode constants for CreateOpts.WaitMode.
const (
	WaitSSH       = "ssh"
	WaitAgent     = "agent"
	WaitUserdata  = "userdata"
	WaitBootstrap = "bootstrap"
)

// CreateOpts for launching a VM.
type CreateOpts struct {
	Name       string
	Persistent bool
	CPUs       int
	MemoryMB   int
	DiskGB     int
	Image      string
	Tags       map[string]string
	// Userdata is optional first-boot cloud-init: a shell snippet (appended as
	// runcmd) or a full #cloud-config document (structure-merged into the base).
	Userdata string
	// Forwards are optional extra host→guest port mappings at create time.
	Forwards []PortForward
	// Mounts are optional host directory shares (virtio-9p) at create time.
	Mounts []Mount
	// SocketForwards are optional host→guest Unix socket forwards at create time.
	SocketForwards []SocketForward
	// Arch is guest ISA (arm64|amd64); empty = host.
	Arch string
	// GPU is "" or "virtio".
	GPU string
	// Network is ""|"slirp" or "overlay".
	Network string
	// WaitMode selects readiness: ssh (default), agent, userdata, or bootstrap.
	WaitMode string
	// WaitTimeout overrides ReadyTimeout when > 0.
	WaitTimeout time.Duration
	// OnEvent receives progress phases during Create (optional; not serialized).
	OnEvent func(CreateEvent)
}

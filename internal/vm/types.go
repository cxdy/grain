package vm

import (
	"time"
)

// Status of a sandbox.
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
	HostPort  int    `json:"host_port"`            // 0 = allocate free
	GuestPort int    `json:"guest_port"`
	Proto     string `json:"proto,omitempty"`      // default tcp
}

// Instance is a managed microVM (sandbox or long-lived).
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
	// Forwards are extra hostfwd entries (beyond SSH :22). Host ports with 0
	// are allocated before start and persisted so restarts reuse them.
	Forwards  []PortForward     `json:"forwards,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	Error     string            `json:"error,omitempty"`
	// DiskPath is host path to the root disk image.
	DiskPath string `json:"disk_path,omitempty"`
	// PID of hypervisor process when running.
	PID int `json:"pid,omitempty"`
}

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
	// OnEvent receives progress phases during Create (optional; not serialized).
	OnEvent func(CreateEvent)
}

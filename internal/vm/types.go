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
	Tags       map[string]string `json:"tags,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	Error      string            `json:"error,omitempty"`
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
}

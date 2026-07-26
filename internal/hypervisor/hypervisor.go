package hypervisor

import (
	"context"

	"github.com/cxdy/grain/internal/vm"
)

// Runtime starts and stops a guest process.
type Runtime interface {
	// Start boots the VM. Updates inst fields (PID, IP, SSHPort, AgentPort) as available.
	Start(ctx context.Context, inst *vm.Instance, diskPath string) error
	// Stop terminates the hypervisor process.
	Stop(ctx context.Context, inst *vm.Instance) error
	// Running reports whether the process is still alive.
	Running(inst *vm.Instance) bool
}

// Disk does base image setup and CoW clones for ephemeral disks.
type Disk interface {
	// EnsureBase prepares the base image under dataDir/images.
	EnsureBase(ctx context.Context, image string) (baseDisk string, err error)
	// Clone creates a new disk for name (APFS clonefile when possible).
	Clone(ctx context.Context, baseDisk, destPath string, sizeGB int) error
}

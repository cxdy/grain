package hypervisor

import (
	"context"

	"github.com/cxdy/grain/internal/vm"
)

// Runtime starts and stops a guest process.
type Runtime interface {
	Start(ctx context.Context, inst *vm.Instance, diskPath string) error
	Stop(ctx context.Context, inst *vm.Instance) error
	Pause(ctx context.Context, inst *vm.Instance) error
	Resume(ctx context.Context, inst *vm.Instance) error
	Running(inst *vm.Instance) bool
}

// Disk does base image setup and CoW clones for ephemeral disks.
type Disk interface {
	EnsureBase(ctx context.Context, image string) (baseDisk string, err error)
	Clone(ctx context.Context, baseDisk, destPath string, sizeGB int) error
}

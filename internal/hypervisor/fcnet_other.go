//go:build !linux

package hypervisor

import (
	"fmt"

	"github.com/cxdy/grain/internal/vm"
)

// SetupFCNet is only available on Linux.
func SetupFCNet(plan FCNetPlan, hostSSH, hostAgent int, fwds []vm.PortForward) (FCNetState, error) {
	_ = plan
	_ = hostSSH
	_ = hostAgent
	_ = fwds
	return FCNetState{}, fmt.Errorf("firecracker networking requires linux")
}

// TeardownFCNet is a no-op on non-Linux.
func TeardownFCNet(st FCNetState) error {
	_ = st
	return nil
}

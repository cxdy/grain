package hypervisor

import (
	"fmt"

	"github.com/cxdy/grain/internal/vm"
)

// virtio9pArgs builds QEMU -fsdev / -device pairs for each host directory mount.
// Uses security_model=mapped-xattr (macOS-friendly; not passthrough).
func virtio9pArgs(mounts []vm.Mount) []string {
	if len(mounts) == 0 {
		return nil
	}
	args := make([]string, 0, len(mounts)*4)
	for i, m := range mounts {
		if m.Host == "" || m.Tag == "" {
			continue
		}
		id := fmt.Sprintf("fs%d", i)
		args = append(args,
			"-fsdev", fmt.Sprintf("local,id=%s,path=%s,security_model=mapped-xattr", id, m.Host),
			"-device", fmt.Sprintf("virtio-9p-pci,fsdev=%s,mount_tag=%s", id, m.Tag),
		)
	}
	return args
}

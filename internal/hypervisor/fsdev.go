package hypervisor

import (
	"fmt"
	"log/slog"
	"runtime"

	"github.com/cxdy/grain/internal/vm"
)

// MountDriver9p is the default virtio-9p shared filesystem.
const MountDriver9p = "9p"

// MountDriverVirtioFS requests virtiofs (falls back to 9p when unsupported).
const MountDriverVirtioFS = "virtiofs"

// ResolveMountDriver returns the effective driver. virtiofs is not fully
// integrated (no virtiofsd lifecycle); on darwin or when unsupported it falls
// back to 9p and logs a warning.
func ResolveMountDriver(requested string, log *slog.Logger) string {
	if requested == "" || requested == MountDriver9p {
		return MountDriver9p
	}
	if requested == MountDriverVirtioFS {
		// Full virtiofsd + QEMU virtio-fs is optional; HVF + virtiofs is not
		// reliable without a host virtiofsd. Fall back to 9p with a warn.
		if log != nil {
			log.Warn("mount_driver=virtiofs is not fully supported; falling back to 9p",
				"goos", runtime.GOOS,
				"hint", "9p is the default and works with QEMU HVF on macOS")
		}
		return MountDriver9p
	}
	return MountDriver9p
}

// fsdevArgs builds QEMU shared-fs device args for mounts using the resolved driver.
// Currently always emits virtio-9p (mapped-xattr); virtiofs is a documented stub
// that falls back via ResolveMountDriver.
func fsdevArgs(mounts []vm.Mount, driver string) []string {
	_ = driver // reserved for future virtiofsd wiring
	return virtio9pArgs(mounts)
}

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

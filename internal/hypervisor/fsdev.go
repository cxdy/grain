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

// ResolveMountDriver returns the effective shared-fs driver.
//
// Rules:
//   - empty / "9p" / unknown → 9p
//   - "virtiofs" on darwin → 9p (HVF + memory-backend/vhost-user not reliable)
//   - "virtiofs" on linux without virtiofsd → 9p + warn
//   - "virtiofs" on linux with virtiofsd present → virtiofs
func ResolveMountDriver(requested string, log *slog.Logger) string {
	if requested == "" || requested == MountDriver9p {
		return MountDriver9p
	}
	if requested != MountDriverVirtioFS {
		return MountDriver9p
	}

	// macOS: keep forced 9p — memory-backend share + vhost-user-fs conflicts
	// with the usual HVF machine setup and host virtiofsd is uncommon.
	if runtime.GOOS == "darwin" {
		if log != nil {
			log.Warn("mount_driver=virtiofs is not supported on darwin; falling back to 9p",
				"goos", runtime.GOOS,
				"hint", "9p is the default and works with QEMU HVF on macOS")
		}
		return MountDriver9p
	}

	if !VirtiofsdAvailable() {
		if log != nil {
			log.Warn("mount_driver=virtiofs requested but virtiofsd not found; falling back to 9p",
				"goos", runtime.GOOS,
				"hint", "install virtiofsd and ensure it is on PATH (or /usr/libexec/virtiofsd)")
		}
		return MountDriver9p
	}

	return MountDriverVirtioFS
}

// fsdevArgs builds QEMU shared-fs device args for mounts using the resolved driver.
// For virtiofs, vmDir is required (socket paths); memory-backend args are separate
// (see virtiofsMemoryBackendArgs) and must be added once by the caller.
// When driver is 9p or mounts are empty, vmDir is ignored.
func fsdevArgs(mounts []vm.Mount, driver string, vmDir string) []string {
	if driver == MountDriverVirtioFS {
		return virtiofsDeviceArgs(mounts, vmDir)
	}
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

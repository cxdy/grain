package hypervisor

import (
	"hash/fnv"
	"os"
	"strings"
)

// Agent transport modes for host ↔ guest grain-agent connectivity.
const (
	AgentTransportAuto  = "auto"
	AgentTransportTCP   = "tcp"
	AgentTransportVsock = "vsock"
)

// VhostVsockDevice is the host device required for QEMU vhost-vsock-pci.
const VhostVsockDevice = "/dev/vhost-vsock"

// MinGuestCID is the first usable guest context ID (0–2 are reserved).
const MinGuestCID = 3

// ResolveAgentTransport selects "vsock" or "tcp" from config and host capability.
// requested is auto|tcp|vsock (empty treated as auto).
// vhostAvailable should reflect whether VhostVsockDevice exists (injectable for tests).
func ResolveAgentTransport(requested string, vhostAvailable bool) string {
	switch strings.ToLower(strings.TrimSpace(requested)) {
	case AgentTransportTCP:
		return AgentTransportTCP
	case AgentTransportVsock:
		return AgentTransportVsock
	default: // auto, empty, unknown
		if vhostAvailable {
			return AgentTransportVsock
		}
		return AgentTransportTCP
	}
}

// VhostVsockAvailable reports whether the host has a *usable* /dev/vhost-vsock
// (typically Linux with vhost_vsock loaded and device permissions). Always false
// on macOS HVF. GitHub-hosted runners sometimes expose the node without write
// access — existence alone is not enough (QEMU then fails with Permission denied).
func VhostVsockAvailable() bool {
	return vhostVsockAvailable(VhostVsockDevice)
}

func vhostVsockAvailable(path string) bool {
	if path == "" {
		path = VhostVsockDevice
	}
	// Must be openable read/write — QEMU needs that for vhost-vsock-pci.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// AllocateGuestCID picks a stable guest context ID for name in [MinGuestCID, …).
// Deterministic so restarts of the same VM reuse the CID; collisions across
// concurrent VMs are rare (fnv32 space) and surface as QEMU start errors.
func AllocateGuestCID(name string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	// Map into a large positive range starting at MinGuestCID.
	const span = 1<<24 - MinGuestCID
	cid := MinGuestCID + int(h.Sum32()%uint32(span))
	if cid < MinGuestCID {
		return MinGuestCID
	}
	return cid
}

package image

import (
	"fmt"
	"runtime"
)

// Spec describes a downloadable base image.
type Spec struct {
	ID          string
	Description string
	// URL for current GOARCH (empty if unsupported).
	URL    string
	SHA256 string // optional; empty skips verify
	// Format: qcow2 | raw
	Format string
	// Default SSH user after cloud-init.
	SSHUser string
	// Size hint for progress (bytes), 0 unknown.
	SizeHint int64
}

// Catalog of built-in images. Prefer cloud images with cloud-init.
func Catalog() map[string]Spec {
	arch := runtime.GOARCH
	c := map[string]Spec{}

	// Ubuntu 24.04 minimal cloud — cloud-init, works with UEFI+QEMU.
	switch arch {
	case "arm64":
		c["ubuntu-cloud"] = Spec{
			ID:          "ubuntu-cloud",
			Description: "Ubuntu 24.04 minimal cloud (arm64)",
			URL:         "https://cloud-images.ubuntu.com/minimal/releases/noble/release/ubuntu-24.04-minimal-cloudimg-arm64.img",
			Format:      "qcow2",
			SSHUser:     "ubuntu",
			SizeHint:    300 * 1024 * 1024,
		}
	case "amd64":
		c["ubuntu-cloud"] = Spec{
			ID:          "ubuntu-cloud",
			Description: "Ubuntu 24.04 minimal cloud (amd64)",
			URL:         "https://cloud-images.ubuntu.com/minimal/releases/noble/release/ubuntu-24.04-minimal-cloudimg-amd64.img",
			Format:      "qcow2",
			SSHUser:     "ubuntu",
			SizeHint:    300 * 1024 * 1024,
		}
	}

	// Alpine virt ISO is not a disk image; keep a documented id for future rootfs builds.
	c["alpine-cloud"] = Spec{
		ID:          "alpine-cloud",
		Description: "Alpine (placeholder — use ubuntu-cloud for now)",
		URL:         "",
		Format:      "raw",
		SSHUser:     "alpine",
	}

	return c
}

// Get returns a catalog entry or error.
func Get(id string) (Spec, error) {
	s, ok := Catalog()[id]
	if !ok {
		return Spec{}, fmt.Errorf("unknown image %q (try: grain image ls)", id)
	}
	return s, nil
}

// DefaultID is the recommended image for this arch.
func DefaultID() string {
	if _, ok := Catalog()["ubuntu-cloud"]; ok {
		return "ubuntu-cloud"
	}
	return "alpine-cloud"
}

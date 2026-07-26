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
	SHA256 string // optional; empty skips verify (dev only)
	// Format: qcow2 | raw
	Format string
	// Default SSH user after cloud-init.
	SSHUser string
	// Size hint for progress (bytes), 0 unknown.
	SizeHint int64
}

// Digests from https://cloud-images.ubuntu.com/minimal/releases/noble/release/SHA256SUMS
// (ubuntu-24.04-minimal-cloudimg-{arm64,amd64}.img). Refresh when Ubuntu rolls the release pointer.
const (
	ubuntuNobleMinimalArm64SHA256 = "7e938df669e3b1923595eeda97aa28569350c5283e05a835cc912a2486a54934"
	ubuntuNobleMinimalAmd64SHA256 = "d99d1abe3284e568161b3b7dabfbd6cf67956a7f4274b13842f10aa9c7807a2c"
)

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
			SHA256:      ubuntuNobleMinimalArm64SHA256,
			Format:      "qcow2",
			SSHUser:     "ubuntu",
			SizeHint:    300 * 1024 * 1024,
		}
	case "amd64":
		c["ubuntu-cloud"] = Spec{
			ID:          "ubuntu-cloud",
			Description: "Ubuntu 24.04 minimal cloud (amd64)",
			URL:         "https://cloud-images.ubuntu.com/minimal/releases/noble/release/ubuntu-24.04-minimal-cloudimg-amd64.img",
			SHA256:      ubuntuNobleMinimalAmd64SHA256,
			Format:      "qcow2",
			SSHUser:     "ubuntu",
			SizeHint:    300 * 1024 * 1024,
		}
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
	return "ubuntu-cloud"
}

package image

import (
	"fmt"
	"runtime"
)

// Spec describes a downloadable or locally-registered base image.
type Spec struct {
	ID          string
	Description string
	// URL for current GOARCH (empty if unsupported or local-only).
	URL    string
	SHA256 string // optional; empty skips verify (dev only)
	// Format: qcow2 | raw
	Format string
	// Default SSH user after cloud-init.
	SSHUser string
	// Size hint for progress (bytes), 0 unknown.
	SizeHint int64
	// HasAgent is true when the image is expected to ship grain-agent
	// (golden images). Create prefers WaitAgent before SSH deploy.
	HasAgent bool
	// LocalOnly images cannot be pulled; register via grain image import.
	LocalOnly bool
}

// Digests from https://cloud-images.ubuntu.com/minimal/releases/noble/release/SHA256SUMS
// (ubuntu-24.04-minimal-cloudimg-{arm64,amd64}.img). Refresh when Ubuntu rolls the release pointer.
const (
	ubuntuNobleMinimalArm64SHA256 = "7e938df669e3b1923595eeda97aa28569350c5283e05a835cc912a2486a54934"
	ubuntuNobleMinimalAmd64SHA256 = "d99d1abe3284e568161b3b7dabfbd6cf67956a7f4274b13842f10aa9c7807a2c"
)

// Catalog IDs.
const (
	IDUbuntuCloud = "ubuntu-cloud"
	IDGrainUbuntu = "grain-ubuntu"
)

// Catalog of built-in images. Prefer cloud images with cloud-init.
func Catalog() map[string]Spec {
	arch := runtime.GOARCH
	c := map[string]Spec{}

	// Ubuntu 24.04 minimal cloud — cloud-init, works with UEFI+QEMU.
	// Agent is deployed over SSH after boot (HasAgent: false).
	switch arch {
	case "arm64":
		c[IDUbuntuCloud] = Spec{
			ID:          IDUbuntuCloud,
			Description: "Ubuntu 24.04 minimal cloud (arm64)",
			URL:         "https://cloud-images.ubuntu.com/minimal/releases/noble/release/ubuntu-24.04-minimal-cloudimg-arm64.img",
			SHA256:      ubuntuNobleMinimalArm64SHA256,
			Format:      "qcow2",
			SSHUser:     "ubuntu",
			SizeHint:    300 * 1024 * 1024,
			HasAgent:    false,
		}
	case "amd64":
		c[IDUbuntuCloud] = Spec{
			ID:          IDUbuntuCloud,
			Description: "Ubuntu 24.04 minimal cloud (amd64)",
			URL:         "https://cloud-images.ubuntu.com/minimal/releases/noble/release/ubuntu-24.04-minimal-cloudimg-amd64.img",
			SHA256:      ubuntuNobleMinimalAmd64SHA256,
			Format:      "qcow2",
			SSHUser:     "ubuntu",
			SizeHint:    300 * 1024 * 1024,
			HasAgent:    false,
		}
	}

	// Golden image with grain-agent baked in. Register via `grain image import`
	// or scripts/bake-golden.sh — no public download URL.
	c[IDGrainUbuntu] = Spec{
		ID:          IDGrainUbuntu,
		Description: "Ubuntu golden image with grain-agent (import or bake locally)",
		URL:         "", // local-only; bootstrap from ubuntu-cloud then import
		Format:      "qcow2",
		SSHUser:     "ubuntu",
		HasAgent:    true,
		LocalOnly:   true,
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

// DefaultID is the recommended catalog image for this arch when no local
// golden image is considered. Config default remains ubuntu-cloud.
func DefaultID() string {
	return IDUbuntuCloud
}

// DefaultIDFor prefers grain-ubuntu when a usable local disk is present under
// dataDir; otherwise returns DefaultID() (ubuntu-cloud).
func DefaultIDFor(dataDir string) string {
	if dataDir != "" {
		m := NewManager(dataDir)
		if m.Ready(IDGrainUbuntu) {
			return IDGrainUbuntu
		}
	}
	return DefaultID()
}

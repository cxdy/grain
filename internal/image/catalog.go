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
	SHA256 string // optional; empty skips verify unless a .sha256 sidecar is available
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
	IDAlpineCloud = "alpine-cloud"
)

// Alpine cloud image release pin (generic UEFI + cloud-init qcow2).
// Alpine no longer publishes separate nocloud_* assets; generic auto-detects
// NoCloud. Login user is "alpine" per https://alpinelinux.org/cloud/
// Refresh version/paths when Alpine rolls a new cloud release.
const (
	alpineCloudVersion = "3.24.1"
	alpineCloudSeries  = "v3.24"
	alpineCloudRev     = "r0"
)

// grainUbuntuReleaseBase is the dedicated GitHub Release tag that the bake
// workflow rewrites with the latest golden qcow2 assets. Asset names:
//
//	grain-ubuntu-amd64.qcow2 / grain-ubuntu-arm64.qcow2
//	(+ companion .sha256 sidecars)
//
// Tag is not a code release (softprops make_latest: false).
const grainUbuntuReleaseBase = "https://github.com/cxdy/grain/releases/download/golden-latest/"

// Catalog of built-in images. Prefer cloud images with cloud-init.
func Catalog() map[string]Spec {
	return catalogFor(runtime.GOARCH)
}

// catalogFor builds the image catalog for arch (amd64/arm64/…). Extracted so
// tests can exercise both arch branches without GOARCH tricks.
func catalogFor(arch string) map[string]Spec {
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

	// Golden image with grain-agent baked in. Published by bake-golden.yml to the
	// golden-latest release tag. Import/bake still work offline without pull.
	// arm64 assets require a self-hosted runner (see docs/images.md); URL is set
	// so pull works once the asset exists.
	grainURL := ""
	switch arch {
	case "amd64", "arm64":
		grainURL = grainUbuntuReleaseBase + "grain-ubuntu-" + arch + ".qcow2"
	}
	c[IDGrainUbuntu] = Spec{
		ID:          IDGrainUbuntu,
		Description: "Ubuntu golden image with grain-agent (pull or import)",
		URL:         grainURL,
		SHA256:      "", // resolved from companion .sha256 sidecar at pull time
		Format:      "qcow2",
		SSHUser:     "ubuntu",
		SizeHint:    400 * 1024 * 1024,
		HasAgent:    true,
		LocalOnly:   grainURL == "", // pullable when arch URL is known
	}

	// Alpine Linux cloud — generic UEFI + cloud-init (NoCloud auto-detected).
	// Agent is deployed over SSH after boot (HasAgent: false). SSH user: alpine.
	// Alpine uses aarch64/x86_64 in filenames (not arm64/amd64).
	// SHA256 left empty: Alpine publishes GPG (.asc) not sha256sum sidecars.
	switch arch {
	case "arm64":
		c[IDAlpineCloud] = Spec{
			ID:          IDAlpineCloud,
			Description: "Alpine Linux " + alpineCloudVersion + " cloud (arm64, UEFI, cloud-init)",
			URL: fmt.Sprintf(
				"https://dl-cdn.alpinelinux.org/alpine/%s/releases/cloud/generic_alpine-%s-aarch64-uefi-cloudinit-%s.qcow2",
				alpineCloudSeries, alpineCloudVersion, alpineCloudRev,
			),
			SHA256:   "",
			Format:   "qcow2",
			SSHUser:  "alpine",
			SizeHint: 240 * 1024 * 1024,
			HasAgent: false,
		}
	case "amd64":
		c[IDAlpineCloud] = Spec{
			ID:          IDAlpineCloud,
			Description: "Alpine Linux " + alpineCloudVersion + " cloud (amd64, UEFI, cloud-init)",
			URL: fmt.Sprintf(
				"https://dl-cdn.alpinelinux.org/alpine/%s/releases/cloud/generic_alpine-%s-x86_64-uefi-cloudinit-%s.qcow2",
				alpineCloudSeries, alpineCloudVersion, alpineCloudRev,
			),
			SHA256:   "",
			Format:   "qcow2",
			SSHUser:  "alpine",
			SizeHint: 200 * 1024 * 1024,
			HasAgent: false,
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

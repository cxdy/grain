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
	URL string
	// SHA256 pin for the downloadable artifact (hex, lowercase preferred).
	// Empty: pull tries companion URL.sha256 sidecar. If both pin and sidecar
	// are missing, pull fails unless AllowUnverified is set (dev/tests only).
	// RISK: AllowUnverified or a silent skip path installs without integrity check.
	SHA256 string
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
	// AllowUnverified permits pull when Spec.SHA256 is empty and no companion
	// .sha256 sidecar is available. Catalog production IDs leave this false
	// (fail closed). Set only for tests or explicit local/dev Specs.
	AllowUnverified bool
}

// Digests from https://cloud-images.ubuntu.com/minimal/releases/noble/release/SHA256SUMS
// (ubuntu-24.04-minimal-cloudimg-{arm64,amd64}.img). Refresh when Ubuntu rolls the release pointer.
const (
	ubuntuNobleMinimalArm64SHA256 = "3a42e0355636bcc4820af28f5bd2c9591502613ab238ad4fa6d4c3659c03d9cf"
	ubuntuNobleMinimalAmd64SHA256 = "b3064efb500d71d6ccbe619b1716062b803e285116e040627b430aaee14cced6"
)

// Alpine cloud qcow2 SHA-256 pins (generic UEFI + cloud-init).
// Alpine publishes companion .sha512 (and GPG .asc), not .sha256 sidecars.
// Digests computed from the published qcow2 and cross-checked against the
// official .sha512 files on dl-cdn.alpinelinux.org. Refresh when alpineCloud*
// version/rev constants change.
const (
	alpineCloudArm64SHA256 = "3059a6280977c2122982632e0317c5ddbd39069d46ca1e60480de283091f720f"
	alpineCloudAmd64SHA256 = "20acb6673d31497bc292a8f6a075d98aa47d03cfe79ddf3c811840e60cf6f8c5"
)

// Catalog IDs.
const (
	IDUbuntuCloud = "ubuntu-cloud"
	IDGrainUbuntu = "grain-ubuntu"
	IDAlpineCloud = "alpine-cloud"
	// Firecracker Phase 1 (explicit IDs — not dual-use of qcow2 grain-ubuntu).
	// Entries stay LocalOnly until bake publishes to fc-latest (see fcReleaseBase).
	// See grain-notes firecracker-production-plan Phase 1 / docs guides/firecracker.
	IDGrainUbuntuFC = "grain-ubuntu-fc"
	IDFCKernel      = "fc-kernel"
)

// fcReleaseBase is the planned GitHub Release tag for Firecracker artifacts.
// Bake pipeline (when live) rewrites assets on tag `fc-latest`:
//
//	grain-ubuntu-fc-amd64.raw / grain-ubuntu-fc-arm64.raw (+ .sha256)
//	vmlinux-amd64 / vmlinux-arm64 (+ .sha256)  → installed as kernels/vmlinux
//
// Not a code release (softprops make_latest: false), same pattern as golden-latest.
const fcReleaseBase = "https://github.com/cxdy/grain/releases/download/fc-latest/"

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
	// Golden: digest is not pinned in-tree (assets rewrite on golden-latest).
	// Pull requires the companion .sha256 sidecar from the same release; empty
	// pin + missing sidecar fails closed (no silent skip).
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
	// SHA256 pinned from published qcow2 (Alpine ships .sha512/.asc, not .sha256).
	switch arch {
	case "arm64":
		c[IDAlpineCloud] = Spec{
			ID:          IDAlpineCloud,
			Description: "Alpine Linux " + alpineCloudVersion + " cloud (arm64, UEFI, cloud-init)",
			URL: fmt.Sprintf(
				"https://dl-cdn.alpinelinux.org/alpine/%s/releases/cloud/generic_alpine-%s-aarch64-uefi-cloudinit-%s.qcow2",
				alpineCloudSeries, alpineCloudVersion, alpineCloudRev,
			),
			SHA256:   alpineCloudArm64SHA256,
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
			SHA256:   alpineCloudAmd64SHA256,
			Format:   "qcow2",
			SSHUser:  "alpine",
			SizeHint: 200 * 1024 * 1024,
			HasAgent: false,
		}
	}

	// Firecracker production track (Phase 1): reserved catalog IDs.
	// Prefer explicit FC IDs over dual-use of grain-ubuntu qcow2 so pull/import
	// never confuses QEMU cloud images with FC raw rootfs / vmlinux.
	// LocalOnly until bake publishes digests to fc-latest; URLs are documented
	// for the planned asset names (pull remains refused until LocalOnly flips).
	//
	//   grain-ubuntu-fc → images/<id>/disk.raw (HasAgent)
	//   fc-kernel       → data_dir/kernels/vmlinux (Manager.KernelPath)
	fcRootURL := ""
	fcKernURL := ""
	switch arch {
	case "amd64", "arm64":
		// Reserved for bake-fc publish; leave empty so LocalOnly stays true.
		_ = fcReleaseBase + "grain-ubuntu-fc-" + arch + ".raw"
		_ = fcReleaseBase + "vmlinux-" + arch
	}
	c[IDGrainUbuntuFC] = Spec{
		ID:          IDGrainUbuntuFC,
		Description: "Firecracker raw rootfs with grain-agent (import BYO; pull when fc-latest publishes)",
		URL:         fcRootURL,
		Format:      "raw",
		SSHUser:     "ubuntu",
		HasAgent:    true,
		LocalOnly:   true, // flip false + set URL/SHA when bake ships
	}
	c[IDFCKernel] = Spec{
		ID:          IDFCKernel,
		Description: "Firecracker guest kernel vmlinux (import → kernels/vmlinux; pull when fc-latest publishes)",
		URL:         fcKernURL,
		Format:      "raw",
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

package image

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCatalogGetAndDefault(t *testing.T) {
	t.Parallel()
	if _, err := Get(""); err == nil {
		t.Fatal("empty id")
	}
	if _, err := Get("no-such-image-id-zzz"); err == nil {
		t.Fatal("unknown")
	}
	id := DefaultID()
	if id == "" {
		t.Fatal("default empty")
	}
	spec, err := Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != id {
		t.Fatalf("%+v", spec)
	}
	_ = DefaultIDFor("")
	_ = DefaultIDFor(t.TempDir())
}

func TestCatalogArchVariants(t *testing.T) {
	t.Parallel()
	c := Catalog()
	if len(c) == 0 {
		t.Fatal("empty catalog")
	}
	// Every arch should have grain-ubuntu entry
	gu, ok := c[IDGrainUbuntu]
	if !ok {
		t.Fatal("missing grain-ubuntu")
	}
	if gu.Format != "qcow2" || gu.SSHUser != "ubuntu" || !gu.HasAgent {
		t.Fatalf("%+v", gu)
	}
	// Firecracker Phase 1 scaffold IDs: always present, LocalOnly until bake.
	fcRoot, ok := c[IDGrainUbuntuFC]
	if !ok {
		t.Fatal("missing grain-ubuntu-fc")
	}
	if fcRoot.Format != "raw" || !fcRoot.HasAgent || !fcRoot.LocalOnly || fcRoot.URL != "" {
		t.Fatalf("grain-ubuntu-fc scaffold: %+v", fcRoot)
	}
	if fcRoot.AllowUnverified {
		t.Fatal("grain-ubuntu-fc must not AllowUnverified")
	}
	fcKern, ok := c[IDFCKernel]
	if !ok {
		t.Fatal("missing fc-kernel")
	}
	if fcKern.Format != "raw" || !fcKern.LocalOnly || fcKern.URL != "" || fcKern.HasAgent {
		t.Fatalf("fc-kernel scaffold: %+v", fcKern)
	}
	if _, err := Get(IDGrainUbuntuFC); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(IDFCKernel); err != nil {
		t.Fatal(err)
	}
	// ubuntu-cloud and alpine-cloud present on amd64/arm64
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "arm64":
		u, ok := c[IDUbuntuCloud]
		if !ok {
			t.Fatalf("missing ubuntu-cloud on %s", arch)
		}
		if u.URL == "" || u.SHA256 == "" || u.SSHUser != "ubuntu" {
			t.Fatalf("%+v", u)
		}
		if !strings.Contains(u.URL, arch) && (arch != "arm64" || !strings.Contains(u.URL, "arm64")) {
			// URL uses arm64/amd64 in path
			if arch == "amd64" && !strings.Contains(u.URL, "amd64") {
				t.Fatalf("ubuntu URL arch: %s", u.URL)
			}
		}
		al, ok := c[IDAlpineCloud]
		if !ok {
			t.Fatalf("missing alpine-cloud on %s", arch)
		}
		if al.SSHUser != "alpine" || al.URL == "" {
			t.Fatalf("%+v", al)
		}
		if al.SHA256 == "" || len(al.SHA256) != 64 {
			t.Fatalf("alpine-cloud must pin SHA256: %+v", al)
		}
		if al.AllowUnverified {
			t.Fatal("catalog alpine-cloud must not AllowUnverified")
		}
		// Alpine uses aarch64 / x86_64 in filenames
		if arch == "arm64" && !strings.Contains(al.URL, "aarch64") {
			t.Fatalf("alpine arm url %s", al.URL)
		}
		if arch == "amd64" && !strings.Contains(al.URL, "x86_64") {
			t.Fatalf("alpine amd url %s", al.URL)
		}
		// grain-ubuntu: empty pin (sidecar at pull), fail-closed default
		if gu.AllowUnverified {
			t.Fatal("catalog grain-ubuntu must not AllowUnverified")
		}
		if gu.SHA256 != "" {
			t.Fatalf("grain-ubuntu pin should stay empty (sidecar): %q", gu.SHA256)
		}
		if u.AllowUnverified {
			t.Fatal("catalog ubuntu-cloud must not AllowUnverified")
		}
	default:
		t.Logf("GOARCH %s: ubuntu/alpine may be absent", arch)
	}
}

func TestCatalogForBothArches(t *testing.T) {
	t.Parallel()
	for _, arch := range []string{"amd64", "arm64"} {
		c := catalogFor(arch)
		u, ok := c[IDUbuntuCloud]
		if !ok || u.URL == "" || !strings.Contains(u.URL, arch) {
			t.Fatalf("%s ubuntu-cloud: %+v", arch, u)
		}
		al, ok := c[IDAlpineCloud]
		if !ok || al.URL == "" {
			t.Fatalf("%s alpine: %+v", arch, al)
		}
		if al.SHA256 == "" || len(al.SHA256) != 64 {
			t.Fatalf("%s alpine SHA256: %+v", arch, al)
		}
		if arch == "amd64" {
			if !strings.Contains(al.URL, "x86_64") {
				t.Fatalf("amd alpine url %s", al.URL)
			}
			if al.SHA256 != alpineCloudAmd64SHA256 {
				t.Fatalf("amd alpine digest %s", al.SHA256)
			}
		}
		if arch == "arm64" {
			if !strings.Contains(al.URL, "aarch64") {
				t.Fatalf("arm alpine url %s", al.URL)
			}
			if al.SHA256 != alpineCloudArm64SHA256 {
				t.Fatalf("arm alpine digest %s", al.SHA256)
			}
		}
		gu := c[IDGrainUbuntu]
		if gu.LocalOnly || !strings.Contains(gu.URL, arch) {
			t.Fatalf("grain-ubuntu %s: %+v", arch, gu)
		}
	}
	// unknown arch: no ubuntu/alpine, grain-ubuntu local-only
	c := catalogFor("riscv64")
	if _, ok := c[IDUbuntuCloud]; ok {
		t.Fatal("unexpected ubuntu on riscv")
	}
	if _, ok := c[IDAlpineCloud]; ok {
		t.Fatal("unexpected alpine on riscv")
	}
	if !c[IDGrainUbuntu].LocalOnly || c[IDGrainUbuntu].URL != "" {
		t.Fatalf("riscv grain: %+v", c[IDGrainUbuntu])
	}
}

func TestGetUnknownAndEmpty(t *testing.T) {
	t.Parallel()
	if _, err := Get(""); err == nil {
		t.Fatal("empty id should error")
	}
	if _, err := Get("no-such-image-id-zzz"); err == nil {
		t.Fatal("unknown id should error")
	}
	if _, err := Get("ubuntu-cloud-typo"); err == nil {
		t.Fatal("typo should error")
	}
}

func TestDefaultIDAndDefaultIDFor(t *testing.T) {
	t.Parallel()
	if DefaultID() != IDUbuntuCloud {
		t.Fatalf("DefaultID=%s", DefaultID())
	}
	if DefaultIDFor("") != DefaultID() {
		t.Fatal("empty dataDir should fall back")
	}
	// empty temp dir: no ready grain-ubuntu
	dir := t.TempDir()
	if DefaultIDFor(dir) != IDUbuntuCloud {
		t.Fatalf("want ubuntu-cloud, got %s", DefaultIDFor(dir))
	}
	// plant ready grain-ubuntu
	m := NewManager(dir)
	d := m.Dir(IDGrainUbuntu)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if DefaultIDFor(dir) != IDGrainUbuntu {
		t.Fatalf("want grain-ubuntu when ready, got %s", DefaultIDFor(dir))
	}
	// known id still Getable
	spec, err := Get(DefaultID())
	if err != nil || spec.ID != DefaultID() {
		t.Fatalf("%+v %v", spec, err)
	}
}

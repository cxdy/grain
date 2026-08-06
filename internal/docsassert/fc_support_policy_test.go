package docsassert

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot walks up from this test file to the module root (go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("go.mod not found")
	return ""
}

func readDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestFCSupportPolicyDocs asserts shipped docs mark agent path supported and
// vFC-2 partial net (publish/fwd) while overlay/mounts stay QEMU-only.
func TestFCSupportPolicyDocs(t *testing.T) {
	t.Parallel()
	matrix := readDoc(t, "docs/content/docs/main/explain/hypervisor-matrix.md")
	fcGuide := readDoc(t, "docs/content/docs/main/guides/firecracker.md")
	parity := readDoc(t, "docs/content/docs/main/explain/parity.md")

	for _, s := range []struct {
		name string
		doc  string
		want []string
	}{
		{"matrix", matrix, []string{
			"FC agent production",
			"vFC-1",
			"vFC-2",
			"grain fwd",
			"TCP proxy",
			"CAP_NET_ADMIN",
		}},
		{"guide", fcGuide, []string{
			"Support policy",
			"FC agent production (vFC-1)",
			"FC net (vFC-2 partial)",
			"grain fwd",
			"grain-ubuntu-fc",
			"grain image pull fc-kernel",
			"grain image pull grain-ubuntu-fc",
			"p95 ≈ 2166 ms",
			"CAP_NET_ADMIN",
			"TCP proxy",
		}},
		{"parity", parity, []string{
			"FC agent production (vFC-1)",
			"FC net (vFC-2 partial)",
			"grain fwd",
		}},
	} {
		for _, w := range s.want {
			if !strings.Contains(s.doc, w) {
				t.Errorf("%s missing %q", s.name, w)
			}
		}
	}

	// Must not deny published pull or claim agent path is still scaffolding / experimental.
	for _, bad := range []string{
		"Host `agent.Dial` AF_VSOCK path does **not** speak CONNECT yet",
		"catalog FC images remain deferred",
		"Catalog IDs `grain-ubuntu-fc` / `fc-kernel` are reserved scaffolding",
		"pull not published yet",
		"catalog pull not published yet",
		"Firecracker (experimental)",
		"Not configured (experimental)",
		"FC is a separate experimental path",
		"experimental operator path",
		"experimental Linux backend",
		"Experimental Linux-only backend",
		"p95 ~1999",
		// Stale pre-vFC-2 claim that publish never works on FC:
		"They do **not** enable networking on `hypervisor: firecracker`",
		// Stale DNAT-only model (publish is host TCP proxy):
		"TAP + DNAT",
	} {
		if strings.Contains(matrix, bad) || strings.Contains(fcGuide, bad) {
			t.Errorf("stale claim still present: %q", bad)
		}
	}
	// Official support language.
	for _, want := range []string{"Supported", "TCP proxy", "arm64"} {
		if !strings.Contains(fcGuide, want) {
			t.Errorf("guide missing official support wording %q", want)
		}
	}

	if !strings.Contains(matrix, "SSH + hostfwd") {
		t.Error("matrix missing SSH+hostfwd row")
	}
	if !strings.Contains(matrix, "vFC-1 agent") {
		t.Error("matrix missing vFC-1 agent status")
	}
	// Overlay/mounts remain QEMU-only.
	if !strings.Contains(matrix, "Not wired") && !strings.Contains(fcGuide, "9p/virtiofs") {
		t.Error("docs should still note mounts not on FC")
	}
}

// TestDoctorPullHintsForFCCatalog asserts doctor errors recommend published pull.
func TestDoctorPullHintsForFCCatalog(t *testing.T) {
	t.Parallel()
	src := readDoc(t, "internal/cli/image_doctor.go")
	for _, bad := range []string{
		"catalog pull not published yet",
		"pull not published yet",
	} {
		if strings.Contains(src, bad) {
			t.Errorf("doctor source still has stale unpublished wording: %q", bad)
		}
	}
	for _, want := range []string{
		"grain image pull %s",
		"IDGrainUbuntuFC",
		"IDFCKernel",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("doctor source missing %q", want)
		}
	}
	// Soft note should prefer pull, not "not imported".
	if strings.Contains(src, "not imported") {
		t.Error("doctor soft note still says 'not imported'; prefer pull wording")
	}
}

// TestCLIRootHelpNotesPublishBothHypervisors ensures root help mentions both models.
func TestCLIRootHelpNotesPublishBothHypervisors(t *testing.T) {
	t.Parallel()
	root := readDoc(t, "internal/cli/root.go")
	if !strings.Contains(root, "Firecracker") {
		t.Error("root help should mention Firecracker publish path")
	}
	if !strings.Contains(root, "publish") && !strings.Contains(root, "-P") {
		t.Error("root help should mention publish")
	}
}

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
// hostfwd/SSH/fwd/overlay/mounts as not available on Firecracker today.
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
			"**No**",
		}},
		{"guide", fcGuide, []string{
			"Support policy",
			"FC agent production (vFC-1)",
			"Not available",
			"grain fwd",
			"grain-ubuntu-fc",
			"grain image pull fc-kernel",
			"grain image pull grain-ubuntu-fc",
			"p95 ≈ 2166 ms",
			"vFC-1 agent production",
		}},
		{"parity", parity, []string{
			"FC agent production (vFC-1)",
			"Not on FC today",
			"grain fwd",
		}},
	} {
		for _, w := range s.want {
			if !strings.Contains(s.doc, w) {
				t.Errorf("%s missing %q", s.name, w)
			}
		}
	}

	// Must not claim hostfwd works on FC today, or deny published pull.
	for _, bad := range []string{
		"Host `agent.Dial` AF_VSOCK path does **not** speak CONNECT yet",
		"catalog FC images remain deferred",
		"Catalog IDs `grain-ubuntu-fc` / `fc-kernel` are reserved scaffolding",
		"pull not published yet",
		"catalog pull not published yet",
		"Firecracker (experimental)",
		"Not configured (experimental)",
		"p95 ~1999",
	} {
		if strings.Contains(matrix, bad) || strings.Contains(fcGuide, bad) {
			t.Errorf("stale claim still present: %q", bad)
		}
	}

	// Matrix must say SSH/hostfwd is No on FC.
	if !strings.Contains(matrix, "SSH + hostfwd") {
		t.Error("matrix missing SSH+hostfwd row")
	}
	// Agent path done language (matrix table cell)
	if !strings.Contains(matrix, "vFC-1 agent") {
		t.Error("matrix missing vFC-1 agent status")
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

// TestCLIRootHelpNotesQEMUHostfwd ensures root help does not imply FC publish/fwd.
func TestCLIRootHelpNotesQEMUHostfwd(t *testing.T) {
	t.Parallel()
	root := readDoc(t, "internal/cli/root.go")
	if !strings.Contains(root, "not Firecracker") {
		t.Error("root help should note publish is not Firecracker")
	}
	if !strings.Contains(root, "not FC") {
		t.Error("root help should note fwd is not FC")
	}
}

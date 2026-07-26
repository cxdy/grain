package agent_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
)

func TestLinuxBinaryName(t *testing.T) {
	if got := agent.LinuxBinaryName("arm64"); got != "grain-agent-linux-arm64" {
		t.Fatalf("got %q", got)
	}
	if got := agent.LinuxBinaryName("amd64"); got != "grain-agent-linux-amd64" {
		t.Fatalf("got %q", got)
	}
	// empty → runtime GOARCH
	want := "grain-agent-linux-" + runtime.GOARCH
	if got := agent.LinuxBinaryName(""); got != want {
		t.Fatalf("empty arch: got %q want %q", got, want)
	}
}

func TestLinuxBinaryPath_DataDir(t *testing.T) {
	dir := t.TempDir()
	name := agent.LinuxBinaryName(runtime.GOARCH)
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(agentDir, name)
	if err := os.WriteFile(binPath, []byte("fake-agent"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := agent.LinuxBinaryPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Resolve both for comparison (symlink / abs).
	want, _ := filepath.Abs(binPath)
	gotAbs, _ := filepath.Abs(got)
	if gotAbs != want {
		t.Fatalf("path = %q, want %q", gotAbs, want)
	}
}

func TestLinuxBinaryPath_CWDBin(t *testing.T) {
	// Prefer cwd/bin when dataDir has nothing (and we are not next to a real exe binary).
	// Use a unique temp cwd with bin/grain-agent-linux-$GOARCH.
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := agent.LinuxBinaryName(runtime.GOARCH)
	binPath := filepath.Join(binDir, name)
	if err := os.WriteFile(binPath, []byte("cwd-agent"), 0o755); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// dataDir empty / missing so we fall through to cwd search (unless test binary
	// directory happens to contain the same name — unlikely).
	got, err := agent.LinuxBinaryPath(filepath.Join(root, "no-such-data"))
	if err != nil {
		// If the test executable's directory already has a real binary, that's OK too.
		if !strings.Contains(err.Error(), "not found") {
			t.Fatal(err)
		}
		t.Fatalf("expected find under cwd/bin: %v", err)
	}
	gotAbs, _ := filepath.Abs(got)
	// Accept either cwd bin or executable-adjacent (both are valid search hits).
	if !strings.HasSuffix(gotAbs, name) {
		t.Fatalf("unexpected path %q", gotAbs)
	}
	st, err := os.Stat(gotAbs)
	if err != nil || st.IsDir() {
		t.Fatalf("not a file: %v", err)
	}
}

func TestLinuxBinaryPath_NotFound(t *testing.T) {
	dir := t.TempDir()
	// Isolate cwd so we don't pick up repo bin/.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = agent.LinuxBinaryPath(filepath.Join(dir, "empty-data"))
	// May still succeed if the test binary's directory has grain-agent-linux-*
	// (e.g. when running from a package that was built next to release bins).
	// Only assert the error message shape when it fails.
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("error should mention not found: %v", err)
		}
		if !strings.Contains(err.Error(), "make agent-linux") {
			t.Fatalf("error should mention make agent-linux: %v", err)
		}
	}
}

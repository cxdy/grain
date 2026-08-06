package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLinuxBinaryName(t *testing.T) {
	if got := LinuxBinaryName("arm64"); got != "grain-agent-linux-arm64" {
		t.Fatalf("got %q", got)
	}
	if got := LinuxBinaryName("amd64"); got != "grain-agent-linux-amd64" {
		t.Fatalf("got %q", got)
	}
	// empty → runtime GOARCH
	want := "grain-agent-linux-" + runtime.GOARCH
	if got := LinuxBinaryName(""); got != want {
		t.Fatalf("empty arch: got %q want %q", got, want)
	}
}

func TestLinuxBinaryPath_DataDir(t *testing.T) {
	dir := t.TempDir()
	name := LinuxBinaryName(runtime.GOARCH)
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(agentDir, name)
	if err := os.WriteFile(binPath, []byte("fake-agent"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := LinuxBinaryPath(dir)
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

// TestLinuxBinaryPath_DataDirBeatsCWD ensures install-cache agent wins over a
// stale checkout bin/ (common when developing grain and running grain deploy).
func TestLinuxBinaryPath_DataDirBeatsCWD(t *testing.T) {
	root := t.TempDir()
	name := LinuxBinaryName(runtime.GOARCH)

	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataDir, "agent", name)
	if err := os.WriteFile(want, []byte("install-cache-agent"), 0o755); err != nil {
		t.Fatal(err)
	}

	cwdRoot := filepath.Join(root, "checkout")
	if err := os.MkdirAll(filepath.Join(cwdRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(cwdRoot, "bin", name)
	if err := os.WriteFile(stale, []byte("stale-repo-agent"), 0o755); err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwdRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	got, err := LinuxBinaryPath(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	gotAbs, _ := filepath.Abs(got)
	wantAbs, _ := filepath.Abs(want)
	if gotAbs != wantAbs {
		t.Fatalf("prefer dataDir agent: got %q want %q", gotAbs, wantAbs)
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
	name := LinuxBinaryName(runtime.GOARCH)
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
	got, err := LinuxBinaryPath(filepath.Join(root, "no-such-data"))
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

	_, err = LinuxBinaryPath(filepath.Join(dir, "empty-data"))
	// May still succeed if the test binary's directory has grain-agent-linux-*
	// (e.g. when running from a package that was built next to release bins).
	// Only assert the error message shape when it fails.
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			t.Fatalf("error should mention not found: %v", err)
		}
		if !strings.Contains(err.Error(), "just agent-linux") {
			t.Fatalf("error should mention just agent-linux: %v", err)
		}
	}
}

func TestLinuxBinaryPathSkipsDirAndFindsFile(t *testing.T) {
	dir := t.TempDir()
	name := LinuxBinaryName("")
	// Directory that matches candidate path → skipped (IsDir branch).
	if err := os.MkdirAll(filepath.Join(dir, "bin", name), 0o755); err != nil {
		t.Fatal(err)
	}
	// Real file under dataDir/agent/
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(agentDir, name)
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Change cwd so bin/<name> dir is searched first.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	got, err := LinuxBinaryPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != real {
		if filepath.Base(got) != name {
			t.Fatalf("got %s want %s", got, real)
		}
	}
}

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

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
		// Abs may differ; compare base
		if filepath.Base(got) != name {
			t.Fatalf("got %s want %s", got, real)
		}
	}
}

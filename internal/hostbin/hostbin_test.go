package hostbin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookPathAbsolute(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "grain-hostbin-test-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := LookPath(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got != bin {
		t.Fatalf("got %q", got)
	}
}

func TestLookPathMissing(t *testing.T) {
	if _, err := LookPath("definitely-not-a-real-grain-tool-xyz-zzz"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestIsExecutable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	okPath := filepath.Join(dir, "ok")
	if err := os.WriteFile(okPath, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !isExecutable(okPath) {
		t.Fatal("expected executable")
	}
	noPath := filepath.Join(dir, "no")
	if err := os.WriteFile(noPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isExecutable(noPath) {
		t.Fatal("expected non-executable")
	}
	if isExecutable(filepath.Join(dir, "missing")) {
		t.Fatal("missing should be false")
	}
}

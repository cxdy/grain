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
	if isExecutable(dir) {
		t.Fatal("directory should be false")
	}
}

func TestLookPathEmpty(t *testing.T) {
	t.Parallel()
	if _, err := LookPath(""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestLookPathRelativeWithSeparator(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "relbin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// path with separator should defer to exec.LookPath
	got, err := LookPath(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
}

func TestLookPathCommonDirsFallback(t *testing.T) {
	dir := t.TempDir()
	name := "grain-hostbin-offpath-test"
	cand := filepath.Join(dir, name)
	if err := os.WriteFile(cand, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := commonDirs
	commonDirs = []string{dir}
	t.Cleanup(func() { commonDirs = old })

	// Name is not on PATH (unique), should hit commonDirs.
	got, err := LookPath(name)
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	if got != cand {
		t.Fatalf("got %q want %q", got, cand)
	}
}

func TestFoundOffPATH(t *testing.T) {
	dir := t.TempDir()
	name := "grain-found-offpath-xyz"
	cand := filepath.Join(dir, name)
	if err := os.WriteFile(cand, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}

	old := commonDirs
	commonDirs = []string{dir}
	t.Cleanup(func() { commonDirs = old })

	// empty / path with separator → false
	if p, ok := FoundOffPATH(""); ok || p != "" {
		t.Fatalf("empty: %q %v", p, ok)
	}
	if p, ok := FoundOffPATH("/bin/sh"); ok || p != "" {
		t.Fatalf("absolute: %q %v", p, ok)
	}

	// sh is almost always on PATH → false
	if p, ok := FoundOffPATH("sh"); ok {
		t.Fatalf("sh on PATH should not report off-path: %q", p)
	}

	// unique name only under commonDirs
	p, ok := FoundOffPATH(name)
	if !ok || p != cand {
		t.Fatalf("FoundOffPATH got %q %v want %q true", p, ok, cand)
	}

	// missing entirely
	if p, ok := FoundOffPATH("definitely-not-grain-tool-zzz-999"); ok || p != "" {
		t.Fatalf("missing: %q %v", p, ok)
	}
}

func TestIsExecutableDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if isExecutable(dir) {
		t.Fatal("directory should not be executable for our purposes")
	}
}

func TestLookPathRelativeMissing(t *testing.T) {
	t.Parallel()
	// relative with separator → exec.LookPath, missing → error (lines covering fallback path)
	if _, err := LookPath(filepath.Join("no", "such", "tool-xyz")); err == nil {
		t.Fatal("expected not found")
	}
}

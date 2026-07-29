package sshkey_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/sshkey"
)

func TestEnsureIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p1, pub1, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub1, "ssh-ed25519 ") {
		t.Fatalf("pub %q", pub1)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatal(err)
	}
	p2, pub2, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 || pub1 != pub2 {
		t.Fatal("keys should be stable")
	}
}

func TestEnsureMissingPubError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sshDir := filepath.Join(dir, "ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_grain"), []byte("fake-priv"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sshkey.Ensure(dir); err == nil {
		t.Fatal("expected error when .pub missing")
	}
}

func TestEnsureFreshAndTrim(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	priv, pub, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub, "ssh-ed25519 ") {
		t.Fatalf("pub %q", pub)
	}
	if strings.HasSuffix(pub, "\n") || strings.HasSuffix(pub, "\r") {
		t.Fatalf("pub should be trimmed: %q", pub)
	}
	if _, err := os.Stat(priv); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(priv + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty pub file")
	}
}

func TestEnsureNestedDataDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	_, pub, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pub == "" {
		t.Fatal("empty pub")
	}
}

func TestEnsureMkdirFails(t *testing.T) {
	t.Parallel()
	// dataDir is a file → MkdirAll under it fails
	base := t.TempDir()
	file := filepath.Join(base, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := sshkey.Ensure(file); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestEnsureWritePrivFails(t *testing.T) {
	t.Parallel()
	// Read-only ssh dir: generate needs to write priv key
	dir := t.TempDir()
	sshDir := filepath.Join(dir, "ssh")
	if err := os.MkdirAll(sshDir, 0o555); err != nil {
		t.Fatal(err)
	}
	// On some systems root can still write; skip if write succeeds
	if f, err := os.OpenFile(filepath.Join(sshDir, "probe"), os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
		_ = os.Remove(filepath.Join(sshDir, "probe"))
		t.Skip("filesystem allows write despite 0555")
	}
	if _, _, err := sshkey.Ensure(dir); err == nil {
		t.Fatal("expected write error")
	}
}

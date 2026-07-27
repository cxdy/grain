package sshkey_test

import (
	"github.com/cxdy/grain/internal/sshkey"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

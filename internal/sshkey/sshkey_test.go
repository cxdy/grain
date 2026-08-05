package sshkey

import (
	"crypto"
	"crypto/ed25519"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestEnsureIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p1, pub1, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub1, "ssh-ed25519 ") {
		t.Fatalf("pub %q", pub1)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatal(err)
	}
	p2, pub2, err := Ensure(dir)
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
	if _, _, err := Ensure(dir); err == nil {
		t.Fatal("expected error when .pub missing")
	}
}

func TestEnsureFreshAndTrim(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	priv, pub, err := Ensure(dir)
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
	_, pub, err := Ensure(dir)
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
	if _, _, err := Ensure(file); err == nil {
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
	if _, _, err := Ensure(dir); err == nil {
		t.Fatal("expected write error")
	}
}

func TestEnsureReuseAfterGenerate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	priv1, pub1, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	// pub has trailing newline in file; Ensure trims
	raw, err := os.ReadFile(priv1 + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		// still ok if no newline
		t.Logf("pub file: %q", raw)
	}
	priv2, pub2, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if priv1 != priv2 || pub1 != pub2 {
		t.Fatalf("reuse mismatch %q %q vs %q %q", priv1, pub1, priv2, pub2)
	}
	// missing pub while priv exists
	if err := os.Remove(priv1 + ".pub"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Ensure(dir); err == nil {
		t.Fatal("expected missing pub error")
	}
}

func TestEnsureGenerateKeyFails(t *testing.T) {
	old := generateKey
	generateKey = func(io.Reader) (ed25519.PublicKey, ed25519.PrivateKey, error) {
		return nil, nil, errors.New("rng fail")
	}
	t.Cleanup(func() { generateKey = old })
	if _, _, err := Ensure(t.TempDir()); err == nil {
		t.Fatal("expected generate error")
	}
}

func TestEnsureMarshalFails(t *testing.T) {
	old := marshalPrivateKey
	marshalPrivateKey = func(crypto.PrivateKey, string) (*pem.Block, error) {
		return nil, errors.New("marshal fail")
	}
	t.Cleanup(func() { marshalPrivateKey = old })
	if _, _, err := Ensure(t.TempDir()); err == nil || !strings.Contains(err.Error(), "marshal private key") {
		t.Fatalf("got %v", err)
	}
}

func TestEnsureNewPublicKeyFails(t *testing.T) {
	old := newPublicKey
	newPublicKey = func(interface{}) (ssh.PublicKey, error) {
		return nil, errors.New("pub fail")
	}
	t.Cleanup(func() { newPublicKey = old })
	if _, _, err := Ensure(t.TempDir()); err == nil {
		t.Fatal("expected NewPublicKey error")
	}
}

func TestEnsureWritePubFails(t *testing.T) {
	old := writeFile
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasSuffix(name, ".pub") {
			return errors.New("pub write fail")
		}
		return os.WriteFile(name, data, perm)
	}
	t.Cleanup(func() { writeFile = old })
	if _, _, err := Ensure(t.TempDir()); err == nil {
		t.Fatal("expected pub write error")
	}
}

func TestBytesTrimCRLF(t *testing.T) {
	t.Parallel()
	if got := bytesTrim([]byte("hi\r\n")); got != "hi" {
		t.Fatalf("%q", got)
	}
	if got := bytesTrim([]byte("ok")); got != "ok" {
		t.Fatalf("%q", got)
	}
}

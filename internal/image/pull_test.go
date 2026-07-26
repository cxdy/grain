package image_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cxdy/grain/internal/image"
)

func TestCatalogGet(t *testing.T) {
	t.Parallel()
	id := image.DefaultID()
	if id == "" {
		t.Fatal("empty default")
	}
	s, err := image.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if s.SSHUser == "" {
		t.Fatal("ssh user")
	}
	if s.ID != id {
		t.Fatalf("id %q want %q", s.ID, id)
	}
	// ubuntu-cloud on arm64/amd64 must pin a digest
	switch runtime.GOARCH {
	case "arm64", "amd64":
		if s.URL == "" {
			t.Fatal("expected URL for ubuntu-cloud on this arch")
		}
		if s.SHA256 == "" {
			t.Fatal("expected SHA256 pin for ubuntu-cloud")
		}
		if len(s.SHA256) != 64 {
			t.Fatalf("SHA256 length %d want 64", len(s.SHA256))
		}
	}
}

func TestCatalogUnknown(t *testing.T) {
	t.Parallel()
	_, err := image.Get("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCatalogNoAlpinePlaceholder(t *testing.T) {
	t.Parallel()
	cat := image.Catalog()
	if _, ok := cat["alpine-cloud"]; ok {
		t.Fatal("alpine-cloud placeholder should be removed from catalog")
	}
}

func TestVerifySHA256Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.partial")
	payload := []byte("grain-image-verify-ok")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])

	if err := image.VerifySHA256(path, want); err != nil {
		t.Fatalf("verify success: %v", err)
	}
	// file must still exist
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file removed on success: %v", err)
	}
}

func TestVerifySHA256MismatchDeletes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.partial")
	if err := os.WriteFile(path, []byte("wrong-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	err := image.VerifySHA256(path, want)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected partial deleted on mismatch, stat=%v err=%v", statErr, err)
	}
}

func TestVerifySHA256EmptySkips(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.partial")
	if err := os.WriteFile(path, []byte("dev"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := image.VerifySHA256(path, ""); err != nil {
		t.Fatalf("empty want should skip: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestDiskPathReady(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := image.NewManager(dir)
	if m.Ready("testimg") {
		t.Fatal("expected not ready")
	}
	imgDir := filepath.Join(dir, "images", "testimg")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 2*1024*1024)
	dest := filepath.Join(imgDir, "disk.qcow2")
	if err := os.WriteFile(dest, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.Ready("testimg") {
		t.Fatal("expected ready")
	}
	p, err := m.DiskPath("testimg")
	if err != nil || p != dest {
		t.Fatalf("path %s err %v", p, err)
	}
}

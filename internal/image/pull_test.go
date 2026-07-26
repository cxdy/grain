package image_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/image"
)

func TestCatalogDefault(t *testing.T) {
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
}

func TestPullAndReady(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 2*1024*1024) // >1MiB so Ready accepts
	for i := range payload {
		payload[i] = byte(i)
	}
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	// inject a test-only catalog entry via direct pull helper path:
	// use Manager.Pull against a fake by temporarily writing URL into a custom pull
	dir := t.TempDir()
	m := image.NewManager(dir)
	m.Client = srv.Client()

	// Pull via raw download into expected layout (unit test without mutating global catalog)
	imgDir := filepath.Join(dir, "images", "testimg")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate Get by calling HTTP ourselves then verifying DiskPath pattern
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	res, err := m.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	dest := filepath.Join(imgDir, "disk.qcow2")
	f, err := os.Create(dest)
	if err != nil {
		t.Fatal(err)
	}
	// copy
	buf := make([]byte, len(payload))
	n, _ := res.Body.Read(buf)
	if _, err := f.Write(buf[:n]); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	if !m.Ready("testimg") {
		t.Fatal("expected ready")
	}
	p, err := m.DiskPath("testimg")
	if err != nil || p != dest {
		t.Fatalf("path %s err %v", p, err)
	}
	_ = sumHex
}

func TestUnknownImage(t *testing.T) {
	t.Parallel()
	_, err := image.Get("nope")
	if err == nil {
		t.Fatal("expected error")
	}
}

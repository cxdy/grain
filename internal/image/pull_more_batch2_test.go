package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullSpecQcow2AndProgress(t *testing.T) {
	payload := bytesRepeat(2*1024*1024 + 100) // > 1MiB for Ready
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(digest + "  disk.qcow2\n"))
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	m := NewManager(dir)
	m.Client = srv.Client()
	var lastWritten, lastTotal int64
	err := m.pullSpec(context.Background(), Spec{
		ID: "qcow-pull", URL: srv.URL + "/disk.qcow2", Format: "qcow2", SHA256: digest,
	}, func(w, total int64) {
		lastWritten, lastTotal = w, total
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastWritten <= 0 {
		t.Fatal("progress not called")
	}
	_ = lastTotal
	if !m.Ready("qcow-pull") {
		t.Fatal("not ready")
	}
	// second pull is no-op
	if err := m.pullSpec(context.Background(), Spec{
		ID: "qcow-pull", URL: srv.URL + "/disk.qcow2", Format: "qcow2",
	}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPullSpecFCKernelWithPin(t *testing.T) {
	payload := []byte("vmlinux-fake-kernel-content-for-tests!!!!")
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	m := NewManager(dir)
	m.Client = srv.Client()
	err := m.pullSpec(context.Background(), Spec{
		ID: IDFCKernel, URL: srv.URL + "/vmlinux", SHA256: digest,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Ready(IDFCKernel) {
		t.Fatal("kernel not ready")
	}
	// provenance file
	if _, err := os.Stat(m.KernelPath() + ".source"); err != nil {
		t.Fatal(err)
	}
}

func TestPullSpecLocalOnlyReady(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	img := filepath.Join(dir, "images", "local-x")
	if err := os.MkdirAll(img, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(img, "disk.raw"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := m.pullSpec(context.Background(), Spec{ID: "local-x", LocalOnly: true}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestResolveWantSHA256Pinned(t *testing.T) {
	got, err := resolveWantSHA256(context.Background(), nil, "", "  ABC  ")
	if err != nil || got != "ABC" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = resolveWantSHA256(context.Background(), nil, "", "")
	if err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}

func bytesRepeat(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiskPathFCKernelAndGrainUbuntuFC(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := NewManager(dir)
	if _, err := m.DiskPath(IDFCKernel); err == nil {
		t.Fatal("missing kernel")
	}
	if _, err := m.DiskPath(IDGrainUbuntuFC); err == nil || !strings.Contains(err.Error(), "not imported") {
		t.Fatalf("%v", err)
	}
	// install kernel
	k := m.KernelPath()
	if err := os.MkdirAll(filepath.Dir(k), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, []byte("kernel-bytes-here"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := m.DiskPath(IDFCKernel)
	if err != nil || got != k {
		t.Fatalf("%s %v", got, err)
	}
}

func TestPullSpecAlreadyReady(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := NewManager(dir)
	imgDir := filepath.Join(dir, "images", "ready-id")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// DiskPath needs > 1MiB
	f, err := os.Create(filepath.Join(imgDir, "disk.qcow2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	err = m.pullSpec(context.Background(), Spec{ID: "ready-id", URL: "http://example.invalid/x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPullSpecMkdirFailAndHTTPStatus(t *testing.T) {
	// block images path with a file so MkdirAll fails for non-kernel
	dir := t.TempDir()
	block := filepath.Join(dir, "images")
	if err := os.WriteFile(block, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	err := m.pullSpec(context.Background(), Spec{
		ID: "x", URL: "http://127.0.0.1:1/nope", SHA256: strings.Repeat("a", 64),
	}, nil)
	if err == nil {
		t.Fatal("want mkdir or download error")
	}

	// HTTP non-200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	dir2 := t.TempDir()
	m2 := NewManager(dir2)
	err = m2.pullSpec(context.Background(), Spec{
		ID: "y", URL: srv.URL + "/disk.img", AllowUnverified: true, Format: "img",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP") {
		t.Fatalf("%v", err)
	}
}

func TestImportKernelTooSmallAndRawSameFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	tiny := filepath.Join(dir, "tiny")
	if err := os.WriteFile(tiny, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Import(context.Background(), IDFCKernel, tiny); err == nil {
		t.Fatal("kernel too small")
	}
	// empty id/src
	if err := m.Import(context.Background(), "", tiny); err == nil {
		t.Fatal("empty id")
	}
	if err := m.Import(context.Background(), IDGrainUbuntu, ""); err == nil {
		t.Fatal("empty src")
	}
}

func TestParseSHA256SidecarInvalidChars(t *testing.T) {
	t.Parallel()
	if ParseSHA256Sidecar(strings.Repeat("g", 64)) != "" {
		t.Fatal("invalid hex")
	}
	if ParseSHA256Sidecar("") != "" {
		t.Fatal("empty")
	}
	// uppercase accepted
	hexUp := strings.Repeat("A", 64)
	if got := ParseSHA256Sidecar(hexUp + "  file"); got != strings.ToLower(hexUp) {
		t.Fatalf("%q", got)
	}
}

func TestFileSHA256AndVerifyMismatchPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	payload := []byte("abc")
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := fileSHA256(p)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(payload)
	if sum != hex.EncodeToString(h[:]) {
		t.Fatalf("%s", sum)
	}
	if err := VerifySHA256(p, strings.Repeat("0", 64)); err == nil {
		t.Fatal("mismatch")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("should delete on mismatch")
	}
}

func TestListLocalWithKernel(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	// empty
	list, err := m.ListLocal()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("%v", list)
	}
	// add kernel
	k := m.KernelPath()
	if err := os.MkdirAll(filepath.Dir(k), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(k, []byte("vmlinux-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err = m.ListLocal()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range list {
		if id == IDFCKernel {
			found = true
		}
	}
	if !found {
		t.Fatalf("%v", list)
	}
	_ = fmt.Sprintf("%v", found)
}

func TestPullUnknownCatalogID(t *testing.T) {
	m := NewManager(t.TempDir())
	if err := m.Pull(context.Background(), "no-such-catalog-id-zzzz", nil); err == nil {
		t.Fatal("want error")
	}
}

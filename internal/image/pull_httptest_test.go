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
	"time"
)

// TestPullSpecWithSHA256Sidecar serves a fake qcow2 + companion .sha256 and
// exercises pullSpec (same path as grain image pull for golden images).
func TestPullSpecWithSHA256Sidecar(t *testing.T) {
	t.Parallel()

	// Payload must be >= 1MiB so Ready/DiskPath accept it.
	payload := make([]byte, 2*1024*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	sum := sha256.Sum256(payload)
	wantHex := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/grain-ubuntu-amd64.qcow2", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/grain-ubuntu-amd64.qcow2.sha256", func(w http.ResponseWriter, r *http.Request) {
		// sha256sum format
		_, _ = fmt.Fprintf(w, "%s  grain-ubuntu-amd64.qcow2\n", wantHex)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	m := NewManager(dir)
	m.Client = &http.Client{Timeout: 30 * time.Second}

	spec := Spec{
		ID:        "test-golden",
		URL:       srv.URL + "/grain-ubuntu-amd64.qcow2",
		SHA256:    "", // force sidecar resolution
		Format:    "qcow2",
		SSHUser:   "ubuntu",
		HasAgent:  true,
		LocalOnly: false,
		SizeHint:  int64(len(payload)),
	}

	var lastWritten int64
	err := m.pullSpec(context.Background(), spec, func(written, total int64) {
		lastWritten = written
		if total > 0 && written > total {
			t.Errorf("written %d > total %d", written, total)
		}
	})
	if err != nil {
		t.Fatalf("pullSpec: %v", err)
	}
	if lastWritten != int64(len(payload)) {
		t.Fatalf("progress written %d want %d", lastWritten, len(payload))
	}
	if !m.Ready(spec.ID) {
		t.Fatal("expected Ready after pull")
	}
	p, err := m.DiskPath(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(payload) {
		t.Fatalf("disk size %d want %d", len(got), len(payload))
	}
	// second pull is a no-op
	if err := m.pullSpec(context.Background(), spec, nil); err != nil {
		t.Fatalf("second pull: %v", err)
	}
	// metadata
	if !m.ImageHasAgent(spec.ID) {
		t.Fatal("HasAgent meta")
	}
	srcURL, err := os.ReadFile(filepath.Join(m.Dir(spec.ID), "source.url"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(srcURL), "grain-ubuntu-amd64.qcow2") {
		t.Fatalf("source.url %q", srcURL)
	}
}

func TestPullSpecFCKernelInstallsVmlinux(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 8*1024)
	for i := range payload {
		payload[i] = byte(i)
	}
	sum := sha256.Sum256(payload)
	wantHex := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/vmlinux-amd64", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	m := NewManager(dir)
	m.Client = &http.Client{Timeout: 10 * time.Second}
	spec := Spec{
		ID:        IDFCKernel,
		URL:       srv.URL + "/vmlinux-amd64",
		SHA256:    wantHex,
		Format:    "raw",
		LocalOnly: false,
	}
	if err := m.pullSpec(context.Background(), spec, nil); err != nil {
		t.Fatal(err)
	}
	if !m.Ready(IDFCKernel) {
		t.Fatal("ready")
	}
	p, err := m.DiskPath(IDFCKernel)
	if err != nil {
		t.Fatal(err)
	}
	if p != m.KernelPath() {
		t.Fatalf("path %s want %s", p, m.KernelPath())
	}
}

func TestPullSpecRawRootfs(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 2*1024*1024)
	sum := sha256.Sum256(payload)
	wantHex := hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/root.raw", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	m := NewManager(dir)
	m.Client = &http.Client{Timeout: 10 * time.Second}
	spec := Spec{
		ID:        "fc-raw-test",
		URL:       srv.URL + "/root.raw",
		SHA256:    wantHex,
		Format:    "raw",
		HasAgent:  true,
		LocalOnly: false,
	}
	if err := m.pullSpec(context.Background(), spec, nil); err != nil {
		t.Fatal(err)
	}
	p, err := m.DiskPath(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "disk.raw" {
		t.Fatalf("want disk.raw got %s", p)
	}
}

func TestPullSpecSidecarMismatch(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 2*1024*1024)
	for i := range payload {
		payload[i] = 7
	}
	badHex := strings.Repeat("aa", 32)

	mux := http.NewServeMux()
	mux.HandleFunc("/disk.qcow2", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/disk.qcow2.sha256", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  disk.qcow2\n", badHex)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 30 * time.Second}
	spec := Spec{
		ID:     "bad-sha",
		URL:    srv.URL + "/disk.qcow2",
		Format: "qcow2",
	}
	err := m.pullSpec(context.Background(), spec, nil)
	if err == nil {
		t.Fatal("expected sha256 mismatch")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("err %v", err)
	}
	if m.Ready(spec.ID) {
		t.Fatal("must not install on mismatch")
	}
}

func TestPullSpecPinnedSHA256(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 2*1024*1024)
	sum := sha256.Sum256(payload)
	wantHex := hex.EncodeToString(sum[:])

	// No sidecar route — pin must be enough.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 30 * time.Second}
	spec := Spec{
		ID:     "pinned",
		URL:    srv.URL + "/img.qcow2",
		SHA256: wantHex,
		Format: "qcow2",
	}
	if err := m.pullSpec(context.Background(), spec, nil); err != nil {
		t.Fatalf("pullSpec pinned: %v", err)
	}
	if !m.Ready(spec.ID) {
		t.Fatal("expected Ready")
	}
}

func TestResolveWantSHA256Sidecar(t *testing.T) {
	t.Parallel()
	want := strings.Repeat("cd", 32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = fmt.Fprintf(w, "%s  file.qcow2\n", want)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	got, err := resolveWantSHA256(context.Background(), srv.Client(), srv.URL+"/file.qcow2", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// pinned wins over sidecar
	got, err = resolveWantSHA256(context.Background(), srv.Client(), srv.URL+"/file.qcow2", strings.Repeat("ee", 32))
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.Repeat("ee", 32) {
		t.Fatalf("pinned: got %q", got)
	}
}

// TestPullSpecFailClosedNoDigest: empty pin + missing sidecar must refuse install.
func TestPullSpecFailClosedNoDigest(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 2*1024*1024)
	mux := http.NewServeMux()
	mux.HandleFunc("/disk.qcow2", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/disk.qcow2.sha256", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 30 * time.Second}
	spec := Spec{
		ID:     "no-digest",
		URL:    srv.URL + "/disk.qcow2",
		SHA256: "",
		Format: "qcow2",
		// AllowUnverified intentionally false — production fail-closed.
	}
	err := m.pullSpec(context.Background(), spec, nil)
	if err == nil {
		t.Fatal("expected refuse unverified pull")
	}
	if !strings.Contains(err.Error(), "refusing unverified pull") {
		t.Fatalf("err %v", err)
	}
	if m.Ready(spec.ID) {
		t.Fatal("must not install without digest")
	}
}

// TestPullSpecAllowUnverifiedEmptyDigest: explicit opt-in skips verify (dev/tests).
func TestPullSpecAllowUnverifiedEmptyDigest(t *testing.T) {
	t.Parallel()

	payload := make([]byte, 2*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 30 * time.Second}
	spec := Spec{
		ID:              "dev-unverified",
		URL:             srv.URL + "/disk.qcow2",
		SHA256:          "",
		Format:          "qcow2",
		AllowUnverified: true,
	}
	if err := m.pullSpec(context.Background(), spec, nil); err != nil {
		t.Fatalf("AllowUnverified pull: %v", err)
	}
	if !m.Ready(spec.ID) {
		t.Fatal("expected Ready")
	}
}

// TestPullSpecSidecarHTTPError: non-404 sidecar failure must not skip verify.
func TestPullSpecSidecarHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 10 * time.Second}
	err := m.pullSpec(context.Background(), Spec{
		ID:     "sidecar-500",
		URL:    srv.URL + "/disk.qcow2",
		Format: "qcow2",
	}, nil)
	if err == nil {
		t.Fatal("expected sidecar HTTP error")
	}
	if !strings.Contains(err.Error(), "sha256 sidecar") {
		t.Fatalf("err %v", err)
	}
}

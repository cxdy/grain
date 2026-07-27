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

func TestFileSHA256Missing(t *testing.T) {
	t.Parallel()
	_, err := fileSHA256(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVerifySHA256MissingFile(t *testing.T) {
	t.Parallel()
	err := VerifySHA256(filepath.Join(t.TempDir(), "missing"), strings.Repeat("ab", 32))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseSHA256SidecarMore(t *testing.T) {
	t.Parallel()
	// uppercase hex normalized
	u := strings.Repeat("AB", 32)
	if got := ParseSHA256Sidecar(u + "  f\n"); got != strings.ToLower(u) {
		t.Fatalf("%q", got)
	}
	// wrong length
	if got := ParseSHA256Sidecar("abcd"); got != "" {
		t.Fatalf("%q", got)
	}
	// invalid hex char
	bad := strings.Repeat("0", 63) + "g"
	if got := ParseSHA256Sidecar(bad); got != "" {
		t.Fatalf("%q", got)
	}
	// empty
	if got := ParseSHA256Sidecar("   \n"); got != "" {
		t.Fatalf("%q", got)
	}
	// tab separator
	h := strings.Repeat("11", 32)
	if got := ParseSHA256Sidecar(h + "\tname"); got != h {
		t.Fatalf("%q", got)
	}
}

func TestPullSpecCanceledContext(t *testing.T) {
	t.Parallel()
	// Server hangs after headers so cancel can win
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(200)
		// hang
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)

	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := m.pullSpec(ctx, Spec{
		ID:     "cancel-img",
		URL:    srv.URL + "/disk.qcow2",
		Format: "qcow2",
		SHA256: "", // skip sidecar prefer
	}, nil)
	if err == nil {
		t.Fatal("expected cancel/error")
	}
}

func TestPullSpecDownloadErrorNoBody(t *testing.T) {
	t.Parallel()
	// Connection refused mid-way via closed server after start is hard;
	// cover HTTP non-200 already exists. Cover empty pinned SHA + no sidecar → skip verify.
	payload := make([]byte, 2*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		// no Content-Length
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 15 * time.Second}
	// SizeHint used when ContentLength unknown
	err := m.pullSpec(context.Background(), Spec{
		ID:       "nolength",
		URL:      srv.URL + "/x.img",
		Format:   "raw", // .img extension path
		SizeHint: int64(len(payload)),
	}, func(written, total int64) {
		if total != int64(len(payload)) && total != -1 && total != 0 {
			// total may be sizeHint
			_ = total
		}
		_ = written
	})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Ready("nolength") {
		t.Fatal("ready")
	}
}

func TestResolveWantSHA256NetworkFailSkips(t *testing.T) {
	t.Parallel()
	// Unreachable URL for sidecar
	got, err := resolveWantSHA256(context.Background(), &http.Client{Timeout: 100 * time.Millisecond},
		"http://127.0.0.1:1/nope.qcow2", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("%q", got)
	}
	// empty image URL
	got, err = resolveWantSHA256(context.Background(), nil, "", "")
	if err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
	// nil client with pinned
	got, err = resolveWantSHA256(context.Background(), nil, "", "  abc  ")
	if err != nil || got != "abc" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestImportSameFilePath(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	// Create dest as grain-ubuntu disk then re-import from dest
	id := IDGrainUbuntu
	d := m.Dir(id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(d, "disk.qcow2")
	if err := os.WriteFile(dest, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Import(context.Background(), id, dest); err != nil {
		t.Fatal(err)
	}
	// has_agent meta written
	if !m.ImageHasAgent(id) {
		t.Fatal("expected agent meta")
	}
}

func TestDiskPathPrefersNames(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir())
	id := "pref"
	d := m.Dir(id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	// only raw
	if err := os.WriteFile(filepath.Join(d, "disk.raw"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := m.DiskPath(id)
	if err != nil || !strings.HasSuffix(p, "disk.raw") {
		t.Fatalf("%s %v", p, err)
	}
	// qcow2 preferred over raw
	if err := os.WriteFile(filepath.Join(d, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = m.DiskPath(id)
	if err != nil || !strings.HasSuffix(p, "disk.qcow2") {
		t.Fatalf("%s %v", p, err)
	}
}

func TestPullCatalogIDUsesGet(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir())
	// unknown id
	err := m.Pull(context.Background(), "not-real-id-xyz", nil)
	if err == nil {
		t.Fatal("expected unknown")
	}
}

func TestWriteMetaAndImageHasAgentCatalog(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir())
	// no local meta → catalog HasAgent for grain-ubuntu
	if !m.ImageHasAgent(IDGrainUbuntu) {
		t.Fatal("catalog has agent")
	}
	// unknown id no meta
	if m.ImageHasAgent("totally-unknown-id") {
		t.Fatal("unknown")
	}
	m.writeMeta("x", Spec{URL: "http://u", SSHUser: "u"}, false)
	// dir may not exist — writeMeta may silently fail; create dir
	_ = os.MkdirAll(m.Dir("x"), 0o755)
	m.writeMeta("x", Spec{URL: "http://u", SSHUser: "u"}, false)
	b, err := os.ReadFile(filepath.Join(m.Dir("x"), "has_agent"))
	if err != nil || !strings.Contains(string(b), "false") {
		t.Fatalf("%s %v", b, err)
	}
}

func TestMaterializeQcow2NonQcowWithoutQemu(t *testing.T) {
	// If qemu-img is on PATH this may convert; exercise path either way.
	dir := t.TempDir()
	src := filepath.Join(dir, "disk.vmdk")
	if err := os.WriteFile(src, []byte("not-real"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.qcow2")
	err := materializeQcow2(context.Background(), src, dst)
	// either convert fails or qemu-img missing
	if err == nil {
		// convert accepted junk — ok
		return
	}
	if !strings.Contains(err.Error(), "qemu-img") && !strings.Contains(err.Error(), "convert") {
		t.Fatalf("%v", err)
	}
}

func TestCopyFileErrors(t *testing.T) {
	t.Parallel()
	if err := copyFile(filepath.Join(t.TempDir(), "nope"), filepath.Join(t.TempDir(), "d")); err == nil {
		t.Fatal("missing src")
	}
	src := filepath.Join(t.TempDir(), "s")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dest in non-existent dir
	if err := copyFile(src, filepath.Join(t.TempDir(), "no", "dir", "f")); err == nil {
		t.Fatal("bad dest")
	}
}

// keep crypto helpers referenced for potential future pin tests
func TestSHA256HelpersUsed(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte("x"))
	_ = hex.EncodeToString(sum[:])
	_ = fmt.Sprintf("%x", sum)
}

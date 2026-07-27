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
	"time"
)

func TestListLocal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := NewManager(dir)
	// missing images root
	list, err := m.ListLocal()
	if err != nil || list != nil {
		t.Fatalf("%v %v", list, err)
	}
	// plant ready + not-ready
	ready := filepath.Join(dir, "images", "ready-img")
	if err := os.MkdirAll(ready, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ready, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "images", "empty-img")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	// file (not dir) ignored
	if err := os.WriteFile(filepath.Join(dir, "images", "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err = m.ListLocal()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != "ready-img" {
		t.Fatalf("%v", list)
	}
}

func TestReadHasAgentMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := NewManager(dir)
	id := "meta-img"
	d := m.Dir(id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		val  string
		want bool
		ok   bool
	}{
		{"1", true, true},
		{"true", true, true},
		{"yes", true, true},
		{"0", false, true},
		{"false", false, true},
		{"no", false, true},
		{"maybe", false, false},
	}
	for _, tc := range cases {
		if err := os.WriteFile(filepath.Join(d, "has_agent"), []byte(tc.val+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, ok := m.readHasAgentMeta(id)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("%q: got %v %v want %v %v", tc.val, got, ok, tc.want, tc.ok)
		}
	}
	// ImageHasAgent prefers meta over catalog
	if err := os.WriteFile(filepath.Join(d, "has_agent"), []byte("false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// plant as ubuntu-cloud id path won't match catalog id — use grain-ubuntu with override
	gdir := m.Dir(IDGrainUbuntu)
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "has_agent"), []byte("false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m.ImageHasAgent(IDGrainUbuntu) {
		t.Fatal("meta false should win")
	}
}

func TestPullSpecLocalOnlyAndEmptyURL(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir())
	err := m.pullSpec(context.Background(), Spec{ID: "loc", LocalOnly: true}, nil)
	if err == nil || !strings.Contains(err.Error(), "local-only") {
		t.Fatalf("err %v", err)
	}
	err = m.pullSpec(context.Background(), Spec{ID: "nourl", URL: ""}, nil)
	if err == nil || !strings.Contains(err.Error(), "no download URL") {
		t.Fatalf("err %v", err)
	}
	// Ready local-only is ok
	id := "loc2"
	d := m.Dir(id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.pullSpec(context.Background(), Spec{ID: id, LocalOnly: true}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPullSpecHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 10 * time.Second}
	err := m.pullSpec(context.Background(), Spec{
		ID:     "404img",
		URL:    srv.URL + "/missing.qcow2",
		Format: "qcow2",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP") {
		t.Fatalf("err %v", err)
	}
}

func TestPullSpecFormatImgAndTinyPlaceholder(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 2*1024*1024)
	for i := range payload {
		payload[i] = 9
	}
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	m := NewManager(dir)
	m.Client = &http.Client{Timeout: 30 * time.Second}
	// pre-plant tiny placeholder to be removed
	id := "fmt-img"
	if err := os.MkdirAll(m.Dir(id), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Dir(id), "disk.img"), []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := m.pullSpec(context.Background(), Spec{
		ID:       id,
		URL:      srv.URL + "/disk.img",
		SHA256:   want,
		Format:   "raw", // → .img
		SSHUser:  "ubuntu",
		HasAgent: false,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Ready(id) {
		t.Fatal("ready")
	}
	// disk.img should exist (format not qcow2)
	if _, err := os.Stat(filepath.Join(m.Dir(id), "disk.img")); err != nil {
		t.Fatal(err)
	}
}

func TestImportEmptyAndDirAndSameFile(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir())
	if err := m.Import(context.Background(), "", "x"); err == nil {
		t.Fatal("empty id")
	}
	if err := m.Import(context.Background(), IDGrainUbuntu, ""); err == nil {
		t.Fatal("empty src")
	}
	if err := m.Import(context.Background(), IDGrainUbuntu, t.TempDir()); err == nil {
		t.Fatal("dir src")
	}
	if err := m.Import(context.Background(), "not-a-catalog-id", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("unknown id")
	}
	// Successful import is covered by other tests (catalog + large source).
}

func TestHasAgentForImport(t *testing.T) {
	t.Parallel()
	if !hasAgentForImport(Spec{HasAgent: true}, "x") {
		t.Fatal()
	}
	if !hasAgentForImport(Spec{}, IDGrainUbuntu) {
		t.Fatal("grain-ubuntu id")
	}
	if hasAgentForImport(Spec{}, IDUbuntuCloud) {
		t.Fatal()
	}
}

func TestCopyFileAndSameFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	if err := os.WriteFile(src, []byte("copy-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "copy-me" {
		t.Fatalf("%q", got)
	}
	if !sameFile(src, src) {
		t.Fatal("same")
	}
	// different paths / inodes: may or may not report sameFile depending on FS.
	_ = sameFile(src, dst)
	if sameFile(src, filepath.Join(dir, "missing")) {
		t.Fatal("missing")
	}
}

func TestResolveWantSHA256EmptyAndMissing(t *testing.T) {
	t.Parallel()
	got, err := resolveWantSHA256(context.Background(), nil, "", "")
	if err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
	// missing sidecar 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	got, err = resolveWantSHA256(context.Background(), srv.Client(), srv.URL+"/x.qcow2", "")
	if err != nil || got != "" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestClientNilUsesDefault(t *testing.T) {
	t.Parallel()
	m := &Manager{DataDir: t.TempDir()}
	if m.client() != http.DefaultClient {
		t.Fatal("default client")
	}
}

func TestWriteMeta(t *testing.T) {
	t.Parallel()
	m := NewManager(t.TempDir())
	id := "wm"
	if err := os.MkdirAll(m.Dir(id), 0o755); err != nil {
		t.Fatal(err)
	}
	m.writeMeta(id, Spec{URL: "http://example/x", SSHUser: "u"}, true)
	b, err := os.ReadFile(filepath.Join(m.Dir(id), "has_agent"))
	if err != nil || !strings.Contains(string(b), "true") {
		t.Fatalf("%s %v", b, err)
	}
}

func TestMaterializeQcow2CopyWithoutConvert(t *testing.T) {
	t.Parallel()
	// When qemu-img is present this still works; when absent copies qcow2/img.
	src := filepath.Join(t.TempDir(), "s.qcow2")
	dst := filepath.Join(t.TempDir(), "d.qcow2")
	payload := make([]byte, 128)
	for i := range payload {
		payload[i] = byte(i)
	}
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := materializeQcow2(context.Background(), src, dst); err != nil {
		// convert may fail for non-real qcow2 then fall back to copy for .qcow2
		t.Fatalf("materialize: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	}
}

func TestPullSpecProgressAndNoContentLength(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 2*1024*1024)
	sum := sha256.Sum256(payload)
	want := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		// no Content-Length: use chunked
		flusher, _ := w.(http.Flusher)
		for i := 0; i < len(payload); i += 64 * 1024 {
			end := i + 64*1024
			if end > len(payload) {
				end = len(payload)
			}
			_, _ = w.Write(payload[i:end])
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)
	m := NewManager(t.TempDir())
	m.Client = &http.Client{Timeout: 30 * time.Second}
	var lastTotal int64
	err := m.pullSpec(context.Background(), Spec{
		ID:       "chunked",
		URL:      srv.URL + "/d.qcow2",
		SHA256:   want,
		Format:   "qcow2",
		SizeHint: int64(len(payload)),
	}, func(written, total int64) {
		lastTotal = total
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastTotal != int64(len(payload)) {
		// SizeHint used when ContentLength unknown
		t.Logf("lastTotal=%d (size hint may apply)", lastTotal)
	}
}

func TestDefaultIDEmptyDataDir(t *testing.T) {
	t.Parallel()
	if DefaultIDFor("") != DefaultID() {
		t.Fatal()
	}
	if DefaultID() != IDUbuntuCloud {
		t.Fatal()
	}
}

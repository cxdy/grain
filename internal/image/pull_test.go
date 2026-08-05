package image_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/image"
)

func TestCatalogGet(t *testing.T) {
	t.Parallel()
	id := image.DefaultID()
	if id == "" {
		t.Fatal("empty default")
	}
	if id != image.IDUbuntuCloud {
		t.Fatalf("DefaultID %q want %q", id, image.IDUbuntuCloud)
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
	if s.HasAgent {
		t.Fatal("ubuntu-cloud should not have HasAgent")
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

func TestCatalogGrainUbuntu(t *testing.T) {
	t.Parallel()
	s, err := image.Get(image.IDGrainUbuntu)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != image.IDGrainUbuntu {
		t.Fatalf("id %q", s.ID)
	}
	if !s.HasAgent {
		t.Fatal("grain-ubuntu should HaveAgent")
	}
	if s.SSHUser != "ubuntu" {
		t.Fatalf("ssh user %q", s.SSHUser)
	}
	if !strings.Contains(s.Description, "grain-agent") {
		t.Fatalf("description should mention grain-agent: %q", s.Description)
	}
	// amd64/arm64: pullable via golden-latest release assets
	switch runtime.GOARCH {
	case "amd64", "arm64":
		if s.LocalOnly {
			t.Fatal("grain-ubuntu should not be LocalOnly when URL is set")
		}
		if s.URL == "" {
			t.Fatal("grain-ubuntu URL should be non-empty on amd64/arm64")
		}
		wantSuffix := "grain-ubuntu-" + runtime.GOARCH + ".qcow2"
		if !strings.HasSuffix(s.URL, wantSuffix) {
			t.Fatalf("URL %q should end with %q", s.URL, wantSuffix)
		}
		if !strings.Contains(s.URL, "golden-latest") {
			t.Fatalf("URL %q should use golden-latest release tag", s.URL)
		}
	default:
		// other arches remain unavailable
		if s.URL != "" {
			t.Fatalf("unexpected URL on %s: %q", runtime.GOARCH, s.URL)
		}
		if !s.LocalOnly {
			t.Fatal("grain-ubuntu without URL should be LocalOnly")
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

func TestCatalogAlpineCloud(t *testing.T) {
	t.Parallel()
	s, err := image.Get(image.IDAlpineCloud)
	if err != nil {
		// alpine-cloud is only registered on arm64/amd64
		switch runtime.GOARCH {
		case "arm64", "amd64":
			t.Fatal(err)
		default:
			return
		}
	}
	if s.ID != image.IDAlpineCloud {
		t.Fatalf("id %q want %q", s.ID, image.IDAlpineCloud)
	}
	if s.HasAgent {
		t.Fatal("alpine-cloud should not have HasAgent")
	}
	if s.SSHUser != "alpine" {
		t.Fatalf("ssh user %q want alpine", s.SSHUser)
	}
	if s.Format != "qcow2" {
		t.Fatalf("format %q want qcow2", s.Format)
	}
	if s.URL == "" {
		t.Fatal("expected URL for alpine-cloud on this arch")
	}
	if !strings.Contains(s.URL, "dl-cdn.alpinelinux.org") {
		t.Fatalf("URL %q should use Alpine CDN", s.URL)
	}
	if !strings.Contains(s.URL, "uefi-cloudinit") {
		t.Fatalf("URL %q should be UEFI cloud-init image", s.URL)
	}
	if !strings.HasSuffix(s.URL, ".qcow2") {
		t.Fatalf("URL %q should end with .qcow2", s.URL)
	}
	switch runtime.GOARCH {
	case "arm64":
		if !strings.Contains(s.URL, "aarch64") {
			t.Fatalf("arm64 URL %q should use aarch64", s.URL)
		}
	case "amd64":
		if !strings.Contains(s.URL, "x86_64") {
			t.Fatalf("amd64 URL %q should use x86_64", s.URL)
		}
	}
	if !strings.Contains(s.Description, "Alpine") {
		t.Fatalf("description should mention Alpine: %q", s.Description)
	}
}

func TestDefaultIDFor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if got := image.DefaultIDFor(dir); got != image.IDUbuntuCloud {
		t.Fatalf("empty dataDir prefer %q got %q", image.IDUbuntuCloud, got)
	}
	// plant grain-ubuntu disk
	imgDir := filepath.Join(dir, "images", image.IDGrainUbuntu)
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 2*1024*1024)
	if err := os.WriteFile(filepath.Join(imgDir, "disk.qcow2"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := image.DefaultIDFor(dir); got != image.IDGrainUbuntu {
		t.Fatalf("with local grain-ubuntu want %q got %q", image.IDGrainUbuntu, got)
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

func TestImportGrainUbuntu(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := image.NewManager(dir)

	// Source qcow2-looking file (>= 1MiB)
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "golden.qcow2")
	payload := make([]byte, 2*1024*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	if m.Ready(image.IDGrainUbuntu) {
		t.Fatal("should not be ready before import")
	}
	if err := m.Import(context.Background(), image.IDGrainUbuntu, src); err != nil {
		t.Fatalf("import: %v", err)
	}
	if !m.Ready(image.IDGrainUbuntu) {
		t.Fatal("expected ready after import")
	}
	p, err := m.DiskPath(image.IDGrainUbuntu)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "images", image.IDGrainUbuntu, "disk.qcow2")
	if p != want {
		t.Fatalf("path %s want %s", p, want)
	}
	// metadata
	if !m.ImageHasAgent(image.IDGrainUbuntu) {
		t.Fatal("ImageHasAgent should be true for grain-ubuntu")
	}
	meta, err := os.ReadFile(filepath.Join(dir, "images", image.IDGrainUbuntu, "has_agent"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(meta)) != "true" {
		t.Fatalf("has_agent meta %q", meta)
	}
	user, err := os.ReadFile(filepath.Join(dir, "images", image.IDGrainUbuntu, "ssh_user"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(user)) != "ubuntu" {
		t.Fatalf("ssh_user %q", user)
	}
}

func TestImportUnknownID(t *testing.T) {
	t.Parallel()
	m := image.NewManager(t.TempDir())
	src := filepath.Join(t.TempDir(), "x.qcow2")
	if err := os.WriteFile(src, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	err := m.Import(context.Background(), "nope", src)
	if err == nil {
		t.Fatal("expected unknown id error")
	}
}

func TestImportTooSmall(t *testing.T) {
	t.Parallel()
	m := image.NewManager(t.TempDir())
	src := filepath.Join(t.TempDir(), "tiny.qcow2")
	if err := os.WriteFile(src, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := m.Import(context.Background(), image.IDGrainUbuntu, src)
	if err == nil {
		t.Fatal("expected too-small error")
	}
}

func TestImportFCKernel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := image.NewManager(dir)
	src := filepath.Join(t.TempDir(), "vmlinux")
	// Kernel floor is 1KiB (not 1MiB rootfs floor).
	if err := os.WriteFile(src, make([]byte, 4*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if m.Ready(image.IDFCKernel) {
		t.Fatal("not ready before import")
	}
	if err := m.Import(context.Background(), image.IDFCKernel, src); err != nil {
		t.Fatal(err)
	}
	if !m.Ready(image.IDFCKernel) {
		t.Fatal("expected ready")
	}
	p, err := m.DiskPath(image.IDFCKernel)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "kernels", "vmlinux")
	if p != want {
		t.Fatalf("path %s want %s", p, want)
	}
	// Too small kernel
	tiny := filepath.Join(t.TempDir(), "tiny")
	if err := os.WriteFile(tiny, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Import(context.Background(), image.IDFCKernel, tiny); err == nil {
		t.Fatal("expected too-small kernel")
	}
}

func TestImportGrainUbuntuFCRaw(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	m := image.NewManager(dir)
	src := filepath.Join(t.TempDir(), "rootfs.ext4")
	if err := os.WriteFile(src, make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Import(context.Background(), image.IDGrainUbuntuFC, src); err != nil {
		t.Fatal(err)
	}
	if !m.Ready(image.IDGrainUbuntuFC) {
		t.Fatal("ready")
	}
	p, err := m.DiskPath(image.IDGrainUbuntuFC)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "images", image.IDGrainUbuntuFC, "disk.raw")
	if p != want {
		t.Fatalf("path %s want %s", p, want)
	}
	if !m.ImageHasAgent(image.IDGrainUbuntuFC) {
		t.Fatal("HasAgent")
	}
}

func TestPullLocalOnlyMissing(t *testing.T) {
	t.Parallel()
	// Local-only path: empty-URL Spec via pullSpec is exercised through
	// Pull when catalog has LocalOnly (ubuntu-cloud is never LocalOnly).
	// Grain-ubuntu is pullable on amd64/arm64; unknown id still errors.
	m := image.NewManager(t.TempDir())
	err := m.Pull(context.Background(), "nope", nil)
	if err == nil {
		t.Fatal("expected unknown image error")
	}
	if !strings.Contains(err.Error(), "unknown image") {
		t.Fatalf("err %v", err)
	}
}

func TestParseSHA256Sidecar(t *testing.T) {
	t.Parallel()
	sum := strings.Repeat("ab", 32) // 64 hex chars
	cases := []struct {
		in   string
		want string
	}{
		{sum + "  grain-ubuntu-amd64.qcow2\n", sum},
		{sum + " *grain-ubuntu-amd64.qcow2", sum},
		{sum + "\n", sum},
		{"  " + sum + "  \n", sum},
		{"not-a-hash", ""},
		{"", ""},
		{strings.Repeat("g", 64), ""},
	}
	for _, tc := range cases {
		got := image.ParseSHA256Sidecar(tc.in)
		if got != tc.want {
			t.Fatalf("parse %q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestImageHasAgentCatalog(t *testing.T) {
	t.Parallel()
	m := image.NewManager(t.TempDir())
	// No local disk: fall back to catalog
	if m.ImageHasAgent(image.IDUbuntuCloud) {
		t.Fatal("ubuntu-cloud catalog HasAgent false")
	}
	if !m.ImageHasAgent(image.IDGrainUbuntu) {
		t.Fatal("grain-ubuntu catalog HasAgent true")
	}
}

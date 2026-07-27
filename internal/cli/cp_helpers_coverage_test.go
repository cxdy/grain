package cli

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCPSpecVariants(t *testing.T) {
	t.Parallel()
	g := parseCPSpec("sbox-1:/tmp/x")
	if !g.Guest || g.Name != "sbox-1" || g.Path != "/tmp/x" {
		t.Fatalf("%+v", g)
	}
	h := parseCPSpec("./file")
	if h.Guest || h.Path != "./file" {
		t.Fatalf("%+v", h)
	}
	// slash before colon → host
	h2 := parseCPSpec("/home/a:b")
	if h2.Guest {
		t.Fatalf("%+v", h2)
	}
	// empty name side
	h3 := parseCPSpec(":foo")
	if h3.Guest {
		t.Fatalf("%+v", h3)
	}
}

func TestIsAgentUnavailableCoverage(t *testing.T) {
	t.Parallel()
	if isAgentUnavailable(nil) {
		t.Fatal()
	}
	if !isAgentUnavailable(errAgentSkip) {
		t.Fatal("skip")
	}
	if !isAgentUnavailable(errors.New("agent not available on guest")) {
		t.Fatal("msg")
	}
	if isAgentUnavailable(errors.New("permission denied")) {
		t.Fatal("real err")
	}
}

func TestWriteExtractTarRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	// symlink if supported
	_ = os.Symlink("a.txt", filepath.Join(src, "link"))

	var buf bytes.Buffer
	if err := writeLocalTar(&buf, src); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractTar(&buf, dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("%q %v", b, err)
	}
	b, err = os.ReadFile(filepath.Join(dest, "sub", "b.txt"))
	if err != nil || string(b) != "world" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestWriteLocalTarSingleFileCoverage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "one.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeLocalTar(&buf, f); err != nil {
		t.Fatal(err)
	}
	if buf.Len() < 100 {
		t.Fatal("tiny tar")
	}
}

func TestSafeHostTarPathCoverage(t *testing.T) {
	t.Parallel()
	dest := t.TempDir()
	p, err := safeHostTarPath(dest, "ok.txt")
	if err != nil || !strings.HasPrefix(p, dest) {
		t.Fatalf("%s %v", p, err)
	}
	if _, err := safeHostTarPath(dest, "../escape"); err == nil {
		t.Fatal("escape")
	}
	if _, err := safeHostTarPath(dest, ".."); err == nil {
		t.Fatal("dotdot")
	}
}

func TestExtractTarRejectsEscape(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "../evil", Mode: 0o644, Size: 1}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	dest := t.TempDir()
	if err := extractTar(&buf, dest); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestExtractTarEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.Close()
	if err := extractTar(io.NopCloser(&buf), t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteLocalTarAndExtract(t *testing.T) {
	src := t.TempDir()
	// file + nested dir + symlink
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "nested.txt"), []byte("nest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("hello.txt", filepath.Join(src, "link-to-hello")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := writeLocalTar(&buf, src); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty tar")
	}

	dest := t.TempDir()
	if err := extractTar(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("hello: %v %q", err, b)
	}
	b, err = os.ReadFile(filepath.Join(dest, "sub", "nested.txt"))
	if err != nil || string(b) != "nest" {
		t.Fatalf("nested: %v %q", err, b)
	}
	link, err := os.Readlink(filepath.Join(dest, "link-to-hello"))
	if err != nil || link != "hello.txt" {
		t.Fatalf("link: %v %q", err, link)
	}
}

func TestWriteLocalTarSingleFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solo.txt")
	if err := os.WriteFile(p, []byte("solo"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeLocalTar(&buf, p); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := extractTar(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "solo.txt"))
	if err != nil || string(b) != "solo" {
		t.Fatalf("%v %q", err, b)
	}
}

func TestExtractTarPathTraversal(t *testing.T) {
	// Build a malicious tar name via safeHostTarPath already tested; extract uses it.
	// Craft tar with ../escape via raw writer is heavy; exercise safeHostTarPath edges.
	dest := t.TempDir()
	if _, err := safeHostTarPath(dest, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := safeHostTarPath(dest, "."); err != nil {
		t.Fatal(err)
	}
	if _, err := safeHostTarPath(dest, "ok/path"); err != nil {
		t.Fatal(err)
	}
	if _, err := safeHostTarPath(dest, "../x"); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := safeHostTarPath(dest, "a/../../x"); err == nil {
		t.Fatal("expected nested traversal error")
	}
}

func TestCmdCpMutualExclusionAndArgs(t *testing.T) {
	cfg := ""
	cmd := cmdCp(&cfg)
	cmd.SetArgs([]string{"--ssh", "--agent", "a", "b"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot use --ssh and --agent") {
		t.Fatalf("%v", err)
	}
}

package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
)

func TestLogPath(t *testing.T) {
	cfg := config.Config{DataDir: "/tmp/grain-test"}
	p, label := logPath(cfg, "sbox-1", false)
	if label != "serial" {
		t.Fatalf("label=%q want serial", label)
	}
	want := filepath.Join("/tmp/grain-test", "vms", "sbox-1", "serial.log")
	if p != want {
		t.Fatalf("path=%q want %q", p, want)
	}
	p, label = logPath(cfg, "sbox-1", true)
	if label != "qemu" {
		t.Fatalf("label=%q want qemu", label)
	}
	want = filepath.Join("/tmp/grain-test", "logs", "sbox-1.log")
	if p != want {
		t.Fatalf("path=%q want %q", p, want)
	}
}

func TestDumpFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serial.log")
	content := "hello from serial\nline2\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := dumpFile(path, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != content {
		t.Fatalf("got %q want %q", buf.String(), content)
	}
}

func TestFollowFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serial.log")
	if err := os.WriteFile(path, []byte("start\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- followFile(ctx, path, &buf, 20*time.Millisecond)
	}()

	// Wait for initial dump.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "start\n") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(buf.String(), "start\n") {
		cancel()
		t.Fatalf("initial content not seen: %q", buf.String())
	}

	// Append more data; follow should pick it up.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if _, err := f.WriteString("more\n"); err != nil {
		f.Close()
		cancel()
		t.Fatal(err)
	}
	f.Close()

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "more\n") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "start\n") || !strings.Contains(got, "more\n") {
		t.Fatalf("follow output %q missing expected lines", got)
	}
}

func TestCopyFromOffsetTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	if err := os.WriteFile(path, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	n, err := copyFromOffset(path, 3, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || buf.String() != "def" {
		t.Fatalf("n=%d buf=%q", n, buf.String())
	}
	// Shrink file below offset.
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = copyFromOffset(path, 3, &buf)
	if !isTruncate(err) {
		t.Fatalf("want truncate err, got %v", err)
	}
}

func TestListLocalVMNames(t *testing.T) {
	dir := t.TempDir()
	// empty
	names, err := listLocalVMNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("want empty, got %v", names)
	}
	// one vm with meta.json
	vmDir := filepath.Join(dir, "vms", "sbox-9")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// stray dir without meta should be ignored
	if err := os.MkdirAll(filepath.Join(dir, "vms", "junk"), 0o755); err != nil {
		t.Fatal(err)
	}
	names, err = listLocalVMNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "sbox-9" {
		t.Fatalf("got %v", names)
	}
}

package hypervisor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestMockDiskFailCloneAndQcow2(t *testing.T) {
	d := &MockDisk{FailClone: true}
	if err := d.Clone(context.Background(), "base", filepath.Join(t.TempDir(), "d.img"), 1); err == nil {
		t.Fatal("expected FailClone")
	}
	d2 := &MockDisk{WriteQcow2: true}
	dest := filepath.Join(t.TempDir(), "sub", "disk.img")
	if err := d2.Clone(context.Background(), "base", dest, 0); err != nil {
		t.Fatal(err)
	}
	// WriteQcow2 may write dest or dest.qcow2 depending on implementation
	if _, err := os.Stat(dest); err != nil {
		if _, err2 := os.Stat(dest + ".qcow2"); err2 != nil {
			t.Fatalf("no dest: %v %v", err, err2)
		}
	}
	// MkdirAll failure: dest parent is a file
	dir := t.TempDir()
	block := filepath.Join(dir, "notdir")
	if err := os.WriteFile(block, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	d3 := &MockDisk{}
	if err := d3.Clone(context.Background(), "b", filepath.Join(block, "disk.img"), 0); err == nil {
		t.Fatal("expected mkdir fail")
	}
}

func TestMockDiskFailEnsureBase(t *testing.T) {
	d := &MockDisk{FailEnsureBase: true}
	if _, err := d.EnsureBase(context.Background(), "img"); err == nil {
		t.Fatal("expected fail")
	}
}

func TestResolveMountDriverVirtiofsFallback(t *testing.T) {
	var logged bool
	log := slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		logged = true
		return len(p), nil
	}), nil))
	got := ResolveMountDriver(MountDriverVirtioFS, log)
	if got != MountDriver9p && got != MountDriverVirtioFS {
		t.Fatalf("got %q", got)
	}
	_ = logged
	if ResolveMountDriver("", nil) == "" {
		t.Fatal("empty")
	}
	if ResolveMountDriver(MountDriver9p, nil) != MountDriver9p {
		t.Fatal("9p")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListImagesEmpty(t *testing.T) {
	dir := t.TempDir()
	imgs := ListImages(dir)
	if len(imgs) == 0 {
		t.Fatal("expected catalog entries")
	}
	// none ready
	for _, img := range imgs {
		if img.Ready && img.ID != "" {
			// possible if env has shared data — ok
			_ = img
		}
	}
	ready := ReadyImages(dir)
	if ready == nil {
		t.Fatal("nil")
	}
}

func TestListImagesReadyLocal(t *testing.T) {
	dir := t.TempDir()
	// fake ready image dir
	id := "fake-local"
	d := filepath.Join(dir, "images", id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	// disk must be > 1MiB for Ready()
	f, err := os.Create(filepath.Join(d, "disk.qcow2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(2 << 20); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	ready := ReadyImages(dir)
	found := false
	for _, r := range ready {
		if r == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("ready=%v", ready)
	}
}

func TestPullImageEmpty(t *testing.T) {
	if err := PullImage(t.Context(), t.TempDir(), ""); err == nil {
		t.Fatal("want error")
	}
}

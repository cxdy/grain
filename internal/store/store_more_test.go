package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func TestStoreDirAndListSkips(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if d := s.Dir("vm1"); d != filepath.Join(root, "vms", "vm1") {
		t.Fatalf("Dir %q", d)
	}

	inst := &vm.Instance{
		Name: "ok", Status: vm.StatusRunning, CPUs: 1, MemoryMB: 512,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Put(inst); err != nil {
		t.Fatal(err)
	}

	vmsRoot := filepath.Join(root, "vms")
	if err := os.WriteFile(filepath.Join(vmsRoot, "notadir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(vmsRoot, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(vmsRoot, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "meta.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "ok" {
		t.Fatalf("list %+v", list)
	}
}

func TestStoreGetCorruptJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	d := s.Dir("broken")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "meta.json"), []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("broken"); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestStorePutOverwriteAndNamesEmpty(t *testing.T) {
	t.Parallel()
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	names, err := s.Names()
	if err != nil || len(names) != 0 {
		t.Fatalf("%v %v", names, err)
	}
	a := &vm.Instance{Name: "a", Status: vm.StatusStopped, CPUs: 1, MemoryMB: 256}
	if err := s.Put(a); err != nil {
		t.Fatal(err)
	}
	a.Status = vm.StatusRunning
	if err := s.Put(a); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("a")
	if err != nil || got.Status != vm.StatusRunning {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.Delete("never-existed"); err != nil {
		t.Fatal(err)
	}
}

func TestStoreNewMkdir(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "nested", "data")
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("nil store")
	}
}

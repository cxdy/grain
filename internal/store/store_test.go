package store_test

import (
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPutGetListDelete(t *testing.T) {
	t.Parallel()
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{
		Name:       "sbox-1",
		Status:     vm.StatusRunning,
		Persistent: false,
		CPUs:       2,
		MemoryMB:   1024,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Put(inst); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("sbox-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "sbox-1" || got.Status != vm.StatusRunning {
		t.Fatalf("%+v", got)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	names, err := s.Names()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := names["sbox-1"]; !ok {
		t.Fatal("names missing")
	}
	if err := s.Delete("sbox-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("sbox-1"); err == nil {
		t.Fatal("expected not found")
	}
}

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

func TestStoreCorruptMetaAndEmptyDir(t *testing.T) {
	dir := t.TempDir()
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{Name: "edge-1", Status: vm.StatusStopped}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("edge-1")
	if err != nil || got.Name != "edge-1" {
		t.Fatalf("%v %v", got, err)
	}
	list, err := st.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	names, err := st.Names()
	if err != nil || len(names) != 1 {
		t.Fatalf("%v %v", names, err)
	}
	if err := st.Delete("edge-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("edge-1"); err == nil {
		t.Fatal("expected missing")
	}
	// corrupt meta
	bad := filepath.Join(dir, "vms", "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "meta.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = st.List()
	_, _ = st.Names()
}

func TestStoreListEmptyDirOnly(t *testing.T) {
	t.Parallel()
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %+v", list)
	}
	names, err := s.Names()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty names, got %v", names)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	t.Parallel()
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Get("does-not-exist")
	if err == nil {
		t.Fatal("expected not found")
	}
}

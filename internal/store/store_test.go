package store_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func TestPutGetListDelete(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// vms/ root is owner-only.
	if st, err := os.Stat(filepath.Join(root, "vms")); err != nil {
		t.Fatal(err)
	} else if perm := st.Mode().Perm(); perm != 0o700 {
		t.Fatalf("vms mode %04o want 0700", perm)
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
	vmDir := s.Dir("sbox-1")
	if st, err := os.Stat(vmDir); err != nil {
		t.Fatal(err)
	} else if perm := st.Mode().Perm(); perm != 0o700 {
		t.Fatalf("vm dir mode %04o want 0700", perm)
	}
	metaPath := filepath.Join(vmDir, "meta.json")
	if st, err := os.Stat(metaPath); err != nil {
		t.Fatal(err)
	} else if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("meta.json mode %04o want 0600", perm)
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

func TestStoreNewFailsWhenDataDirIsFile(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	file := filepath.Join(base, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.New(file); err == nil {
		t.Fatal("expected New error when dataDir is a file")
	}
}

func TestStorePutFailsWhenVMsRootIsFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Create store first (ok), then replace vms/<name> parent path with a file by
	// making the instance name point under a file path... Put MkdirAll on Dir(name).
	// Instead: make root/vms a file after New.
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	vms := filepath.Join(root, "vms")
	if err := os.RemoveAll(vms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vms, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(&vm.Instance{Name: "x", Status: vm.StatusStopped}); err == nil {
		t.Fatal("expected Put error")
	}
}

func TestStoreListNamesSkipNonDirAndCorrupt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// good
	if err := s.Put(&vm.Instance{Name: "good", Status: vm.StatusRunning, CPUs: 1, MemoryMB: 256}); err != nil {
		t.Fatal(err)
	}
	// file entry under vms
	if err := os.WriteFile(filepath.Join(root, "vms", "fileentry"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	// dir without meta
	if err := os.MkdirAll(filepath.Join(root, "vms", "nometa"), 0o755); err != nil {
		t.Fatal(err)
	}
	// corrupt meta
	bad := filepath.Join(root, "vms", "corrupt")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "meta.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "good" {
		t.Fatalf("%+v", list)
	}
	names, err := s.Names()
	if err != nil || len(names) != 1 {
		t.Fatalf("%v %v", names, err)
	}
	if _, ok := names["good"]; !ok {
		t.Fatal(names)
	}
}

func TestRename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(s.Dir("old"), "disk.qcow2")
	qmp := filepath.Join(s.Dir("old"), "qmp.sock")
	if err := os.MkdirAll(s.Dir("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &vm.Instance{
		Name:      "old",
		Status:    vm.StatusStopped,
		DiskPath:  disk,
		QMPPath:   qmp,
		CPUs:      1,
		MemoryMB:  512,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.Put(inst); err != nil {
		t.Fatal(err)
	}
	got, err := s.Rename("old", "new")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "new" {
		t.Fatalf("name %s", got.Name)
	}
	wantDisk := filepath.Join(s.Dir("new"), "disk.qcow2")
	wantQMP := filepath.Join(s.Dir("new"), "qmp.sock")
	if got.DiskPath != wantDisk {
		t.Fatalf("disk %s want %s", got.DiskPath, wantDisk)
	}
	if got.QMPPath != wantQMP {
		t.Fatalf("qmp %s want %s", got.QMPPath, wantQMP)
	}
	if _, err := s.Get("old"); err == nil {
		t.Fatal("old should be gone")
	}
	if g2, err := s.Get("new"); err != nil || g2.DiskPath != wantDisk {
		t.Fatalf("get new: %+v %v", g2, err)
	}
	// conflict
	if err := s.Put(&vm.Instance{Name: "taken", Status: vm.StatusStopped, CPUs: 1, MemoryMB: 256}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename("new", "taken"); err == nil {
		t.Fatal("expected conflict")
	}
}

func TestRenameEdgeCases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename("", "x"); err == nil {
		t.Fatal("empty old")
	}
	if _, err := s.Rename("x", ""); err == nil {
		t.Fatal("empty new")
	}
	if _, err := s.Rename("missing", "other"); err == nil {
		t.Fatal("missing old")
	}
	// same name is identity
	if err := s.Put(&vm.Instance{Name: "same", Status: vm.StatusStopped, CPUs: 1, MemoryMB: 256}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Rename("same", "same")
	if err != nil || got.Name != "same" {
		t.Fatalf("%+v %v", got, err)
	}
	// same name missing
	if _, err := s.Rename("nope", "nope"); err == nil {
		t.Fatal("same name missing")
	}
	// rename with empty disk/qmp paths
	if err := s.Put(&vm.Instance{Name: "plain", Status: vm.StatusStopped, CPUs: 1, MemoryMB: 128}); err != nil {
		t.Fatal(err)
	}
	got, err = s.Rename("plain", "plain2")
	if err != nil || got.Name != "plain2" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestRenameCorruptMetaRollback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// Create VM dir with corrupt meta (Rename moves dir then fails unmarshal → rollback)
	d := s.Dir("broken")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "meta.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rename("broken", "moved"); err == nil {
		t.Fatal("expected corrupt meta error")
	}
	// best-effort rollback should restore old dir
	if _, err := os.Stat(s.Dir("broken")); err != nil {
		t.Fatalf("rollback: %v", err)
	}
}

func TestStoreConcurrentPutGet(t *testing.T) {
	t.Parallel()
	s, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			_ = s.Put(&vm.Instance{Name: "c", Status: vm.StatusRunning, CPUs: 1, MemoryMB: 256})
			_, _ = s.Get("c")
			_, _ = s.List()
			_, _ = s.Names()
		}
	}()
	for i := 0; i < 50; i++ {
		_ = s.Put(&vm.Instance{Name: "c", Status: vm.StatusStopped, CPUs: 2, MemoryMB: 512})
		_, _ = s.Get("c")
		_, _ = s.List()
	}
	<-done
	if _, err := s.Get("c"); err != nil {
		t.Fatal(err)
	}
}

func TestRenameMissingMetaAfterMove(t *testing.T) {
	t.Parallel()
	// Dir exists but meta missing: rename moves dir, ReadFile fails, rollback.
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	d := s.Dir("nometa")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	// no meta.json
	if _, err := s.Rename("nometa", "elsewhere"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(s.Dir("nometa")); err != nil {
		t.Fatalf("should rollback: %v", err)
	}
}

func TestListWhenVMsDirRemoved(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	s, err := store.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "vms")); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || list != nil {
		t.Fatalf("%v %v", list, err)
	}
}

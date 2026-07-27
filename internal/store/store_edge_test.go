package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestStorePutGetListDeleteEdges(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
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

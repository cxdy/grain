package store_test

import (
	"testing"
	"time"

	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
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

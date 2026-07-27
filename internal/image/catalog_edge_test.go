package image

import "testing"

func TestCatalogGetAndDefault(t *testing.T) {
	t.Parallel()
	if _, err := Get(""); err == nil {
		t.Fatal("empty id")
	}
	if _, err := Get("no-such-image-id-zzz"); err == nil {
		t.Fatal("unknown")
	}
	id := DefaultID()
	if id == "" {
		t.Fatal("default empty")
	}
	spec, err := Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != id {
		t.Fatalf("%+v", spec)
	}
	_ = DefaultIDFor("")
	_ = DefaultIDFor(t.TempDir())
}

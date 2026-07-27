package presets_test

import (
	"testing"

	"github.com/cxdy/grain/internal/presets"
)

func TestGetAndListAndResources(t *testing.T) {
	t.Parallel()
	if _, err := presets.Get(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := presets.Get("../etc/passwd"); err == nil {
		t.Fatal("path")
	}
	if _, err := presets.Get("nope-xyz"); err == nil {
		t.Fatal("unknown")
	}
	for _, name := range []string{"docker", "k3s", "act"} {
		s, err := presets.Get(name)
		if err != nil || s == "" {
			t.Fatalf("%s: %v", name, err)
		}
		if s[len(s)-1] != '\n' {
			t.Fatalf("%s missing trailing newline", name)
		}
	}
	list := presets.List()
	if len(list) < 3 {
		t.Fatalf("%v", list)
	}
	// preferred order
	if list[0] != "docker" {
		t.Fatalf("order %v", list)
	}
	cpus, mem := presets.DefaultResources("k3s")
	if cpus == 0 || mem == 0 {
		t.Fatal(cpus, mem)
	}
	cpus, mem = presets.DefaultResources("act")
	if cpus == 0 || mem == 0 {
		t.Fatal(cpus, mem)
	}
	cpus, mem = presets.DefaultResources("docker")
	if cpus != 0 || mem != 0 {
		t.Fatal(cpus, mem)
	}
	if !presets.NeedsAPIServerPort("k3s") {
		t.Fatal("k3s")
	}
	if presets.NeedsAPIServerPort("act") {
		t.Fatal("act")
	}
}

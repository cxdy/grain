package presets_test

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/presets"
)

func TestGetDockerK3sNonEmpty(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"docker", "k3s"} {
		ud, err := presets.Get(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(ud, "#cloud-config") {
			t.Fatalf("%s missing #cloud-config:\n%s", name, ud)
		}
		if len(ud) < 40 {
			t.Fatalf("%s too short: %q", name, ud)
		}
	}
	docker, _ := presets.Get("docker")
	if !strings.Contains(docker, "docker") {
		t.Fatalf("docker preset should mention docker:\n%s", docker)
	}
	k3s, _ := presets.Get("k3s")
	if !strings.Contains(k3s, "k3s") {
		t.Fatalf("k3s preset should mention k3s:\n%s", k3s)
	}
}

func TestGetUnknown(t *testing.T) {
	t.Parallel()
	_, err := presets.Get("nope")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown preset") {
		t.Fatalf("err %v", err)
	}
	_, err = presets.Get("")
	if err == nil {
		t.Fatal("empty name")
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	list := presets.List()
	if len(list) < 2 {
		t.Fatalf("list %v", list)
	}
	found := map[string]bool{}
	for _, n := range list {
		found[n] = true
	}
	if !found["docker"] || !found["k3s"] {
		t.Fatalf("list %v", list)
	}
	// preferred order: docker before k3s
	di, ki := -1, -1
	for i, n := range list {
		if n == "docker" {
			di = i
		}
		if n == "k3s" {
			ki = i
		}
	}
	if di < 0 || ki < 0 || di > ki {
		t.Fatalf("order %v", list)
	}
}

func TestDefaultResourcesK3s(t *testing.T) {
	t.Parallel()
	c, m := presets.DefaultResources("k3s")
	if c != 2 || m != 4096 {
		t.Fatalf("k3s defaults cpus=%d mem=%d", c, m)
	}
	c, m = presets.DefaultResources("docker")
	if c != 0 || m != 0 {
		t.Fatalf("docker should have no forced defaults, got %d %d", c, m)
	}
}

func TestNeedsAPIServerPort(t *testing.T) {
	t.Parallel()
	if !presets.NeedsAPIServerPort("k3s") {
		t.Fatal("k3s should need 6443")
	}
	if presets.NeedsAPIServerPort("docker") {
		t.Fatal("docker should not force 6443")
	}
}

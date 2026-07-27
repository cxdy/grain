package presets_test

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/presets"
)

func TestGetPathTricksRejected(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"../etc/passwd", "a/b", "x\\y", "foo.bar", "docker/../k3s"} {
		_, err := presets.Get(name)
		if err == nil {
			t.Fatalf("expected reject for %q", name)
		}
	}
}

func TestGetCaseAndSpaceNormalize(t *testing.T) {
	t.Parallel()
	ud, err := presets.Get("  Docker  ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud, "#cloud-config") {
		t.Fatalf("%s", ud)
	}
	ud2, err := presets.Get("K3S")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud2, "k3s") {
		t.Fatalf("%s", ud2)
	}
}

func TestDefaultResourcesAndNeedsPortWhitespace(t *testing.T) {
	t.Parallel()
	c, m := presets.DefaultResources("  ACT ")
	if c != 2 || m != 4096 {
		t.Fatalf("%d %d", c, m)
	}
	c, m = presets.DefaultResources("")
	if c != 0 || m != 0 {
		t.Fatalf("%d %d", c, m)
	}
	if !presets.NeedsAPIServerPort("  K3s ") {
		t.Fatal("k3s whitespace")
	}
	if presets.NeedsAPIServerPort("  ") {
		t.Fatal("empty")
	}
}

func TestListStablePreferredOrder(t *testing.T) {
	t.Parallel()
	// Call twice — order stable
	a := presets.List()
	b := presets.List()
	if len(a) != len(b) {
		t.Fatalf("%v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("order drift %v vs %v", a, b)
		}
	}
}

package presets_test

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/presets"
)

func TestGetDockerK3sActNonEmpty(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"docker", "k3s", "act"} {
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
	act, _ := presets.Get("act")
	if !strings.Contains(act, "docker") {
		t.Fatalf("act preset should install docker:\n%s", act)
	}
	if !strings.Contains(act, "nektos/act") && !strings.Contains(act, "/usr/local/bin") {
		t.Fatalf("act preset should install act:\n%s", act)
	}
	if !strings.Contains(act, "actrc") {
		t.Fatalf("act preset should seed actrc for non-interactive runs:\n%s", act)
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
	if !found["docker"] || !found["k3s"] || !found["act"] {
		t.Fatalf("list %v", list)
	}
	// preferred order: docker before k3s before act
	di, ki, ai := -1, -1, -1
	for i, n := range list {
		switch n {
		case "docker":
			di = i
		case "k3s":
			ki = i
		case "act":
			ai = i
		}
	}
	if di < 0 || ki < 0 || ai < 0 || di > ki || ki > ai {
		t.Fatalf("order %v", list)
	}
}

func TestDefaultResourcesK3sAndAct(t *testing.T) {
	t.Parallel()
	c, m := presets.DefaultResources("k3s")
	if c != 2 || m != 4096 {
		t.Fatalf("k3s defaults cpus=%d mem=%d", c, m)
	}
	c, m = presets.DefaultResources("act")
	if c != 2 || m != 4096 {
		t.Fatalf("act defaults cpus=%d mem=%d", c, m)
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
	if presets.NeedsAPIServerPort("act") {
		t.Fatal("act should not force 6443")
	}
}

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

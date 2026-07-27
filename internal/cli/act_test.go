package cli

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/names"
)

func TestSanitizeActName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"hello":    "hello",
		"My Repo!": "my-repo",
		"act-lab":  "act-lab",
		"123foo":   "act-123foo",
		"":         "act",
		"---":      "act",
		"Foo_Bar":  "foo-bar",
	}
	for in, want := range cases {
		got := sanitizeActName(in)
		if got != want {
			t.Errorf("sanitizeActName(%q)=%q want %q", in, got, want)
		}
		if !names.Valid(got) {
			t.Errorf("sanitizeActName(%q)=%q is not names.Valid", in, got)
		}
	}
	long := strings.Repeat("a", 80)
	got := sanitizeActName(long)
	if len(got) > 48 {
		t.Fatalf("len %d", len(got))
	}
	if !names.Valid(got) {
		t.Fatalf("long not valid: %q", got)
	}
}

func TestShellJoin(t *testing.T) {
	t.Parallel()
	if g := shellJoin([]string{"-j", "test"}); g != "-j test" {
		t.Fatalf("%q", g)
	}
	if g := shellJoin([]string{"-W", "path with space.yml"}); !strings.Contains(g, "'") {
		t.Fatalf("expected quoting: %q", g)
	}
	if g := shellJoin([]string{""}); g != "''" {
		t.Fatalf("empty: %q", g)
	}
}

func TestStripLeadingDashDash(t *testing.T) {
	t.Parallel()
	if g := stripLeadingDashDash([]string{"--", "-l"}); len(g) != 1 || g[0] != "-l" {
		t.Fatalf("%v", g)
	}
	if g := stripLeadingDashDash([]string{"-l"}); len(g) != 1 || g[0] != "-l" {
		t.Fatalf("%v", g)
	}
}

func TestApplyPresetAct(t *testing.T) {
	t.Parallel()
	ud, cpus, mem, fwds, err := applyPreset("act", "", 0, 0, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud, "act") || !strings.Contains(ud, "docker") {
		t.Fatalf("userdata:\n%s", ud)
	}
	if cpus != 2 || mem != 4096 {
		t.Fatalf("resources cpus=%d mem=%d", cpus, mem)
	}
	if len(fwds) != 0 {
		t.Fatalf("fwds %v", fwds)
	}
}

package agent

import (
	"strings"
	"testing"
)

func TestShellEnvFromQuery(t *testing.T) {
	t.Parallel()
	got := shellEnvFromQuery(map[string]string{
		"term":                 "xterm-ghostty",
		"term_program":         "ghostty",
		"colorterm":            "truecolor",
		"lang":                 "en_US.UTF-8",
		"term_program_version": "1.0",
	})
	join := strings.Join(got, "\n")
	for _, want := range []string{
		"TERM=xterm-ghostty",
		"TERM_PROGRAM=ghostty",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
		"TERM_PROGRAM_VERSION=1.0",
	} {
		if !strings.Contains(join, want) {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	// Reject empty / control
	if shellEnvFromQuery(map[string]string{"term": "x\ny"}) != nil {
		t.Fatal("expected reject newline")
	}
}

func TestMergeShellEnv(t *testing.T) {
	t.Parallel()
	base := []string{"TERM=xterm-256color", "HOME=/root", "PATH=/bin"}
	out := mergeShellEnv(base, []string{"TERM=xterm-ghostty", "TERM_PROGRAM=iTerm.app"})
	m := map[string]string{}
	for _, kv := range out {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	if m["TERM"] != "xterm-ghostty" {
		t.Fatalf("TERM=%q", m["TERM"])
	}
	if m["TERM_PROGRAM"] != "iTerm.app" {
		t.Fatalf("TERM_PROGRAM=%q", m["TERM_PROGRAM"])
	}
	if m["HOME"] != "/root" {
		t.Fatalf("HOME=%q", m["HOME"])
	}
}

func TestHostShellExtraEnv(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "TestTerm")
	t.Setenv("COLORTERM", "truecolor")
	got := HostShellExtraEnv()
	join := strings.Join(got, "\n")
	if !strings.Contains(join, "TERM_PROGRAM=TestTerm") {
		t.Fatalf("%v", got)
	}
	if !strings.Contains(join, "COLORTERM=truecolor") {
		t.Fatalf("%v", got)
	}
}

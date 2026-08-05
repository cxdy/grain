package agent

import (
	"net/url"
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
		"lc_terminal":          "iTerm2",
		"term_features":        "T3Lr",
		"iterm_session_id":     "w0t0p0:abc",
	})
	join := strings.Join(got, "\n")
	for _, want := range []string{
		"TERM=xterm-ghostty",
		"TERM_PROGRAM=ghostty",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
		"TERM_PROGRAM_VERSION=1.0",
		"LC_TERMINAL=iTerm2",
		"TERM_FEATURES=T3Lr",
		"ITERM_SESSION_ID=w0t0p0:abc",
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

func TestShellEnvToQueryRoundTrip(t *testing.T) {
	t.Parallel()
	q := make(url.Values)
	shellEnvToQuery(q, []string{
		"TERM_PROGRAM=iTerm.app",
		"LC_TERMINAL=iTerm2",
		"TERM_FEATURES=T3LrMSc",
		"COLORTERM=truecolor",
	})
	got := shellEnvFromQuery(map[string]string{
		"term_program":  q.Get("term_program"),
		"lc_terminal":   q.Get("lc_terminal"),
		"term_features": q.Get("term_features"),
		"colorterm":     q.Get("colorterm"),
	})
	join := strings.Join(got, "\n")
	for _, want := range []string{
		"TERM_PROGRAM=iTerm.app",
		"LC_TERMINAL=iTerm2",
		"TERM_FEATURES=T3LrMSc",
		"COLORTERM=truecolor",
	} {
		if !strings.Contains(join, want) {
			t.Fatalf("missing %q in %v (q=%v)", want, got, q)
		}
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

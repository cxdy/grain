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

func TestShellEnvFromQueryEmptyAndCap(t *testing.T) {
	t.Parallel()
	if shellEnvFromQuery(nil) != nil {
		t.Fatal("nil map")
	}
	if shellEnvFromQuery(map[string]string{}) != nil {
		t.Fatal("empty map")
	}
	// reject CR/null in the middle (TrimSpace strips trailing CR/LF first)
	if len(shellEnvFromQuery(map[string]string{"term": "x\ry"})) != 0 {
		t.Fatal("cr")
	}
	if len(shellEnvFromQuery(map[string]string{"term": "a\x00b"})) != 0 {
		t.Fatal("null")
	}
	// length cap at 512
	long := strings.Repeat("a", 600)
	got := shellEnvFromQuery(map[string]string{"term": long})
	if len(got) != 1 {
		t.Fatalf("%v", got)
	}
	_, v, _ := strings.Cut(got[0], "=")
	if len(v) != 512 {
		t.Fatalf("cap len %d", len(v))
	}
}

func TestShellEnvToQueryEmptyAndCase(t *testing.T) {
	t.Parallel()
	q := make(url.Values)
	shellEnvToQuery(q, nil)
	if len(q) != 0 {
		t.Fatal(q)
	}
	shellEnvToQuery(q, []string{})
	shellEnvToQuery(q, []string{"NOEQUALS", "TERM=", "unknown=val", "term_program=lower"})
	if q.Get("term_program") != "lower" {
		t.Fatalf("%v", q)
	}
	// case-insensitive env name
	q2 := make(url.Values)
	shellEnvToQuery(q2, []string{"colorterm=truecolor"})
	if q2.Get("colorterm") != "truecolor" {
		t.Fatalf("%v", q2)
	}
}

func TestMergeShellEnvEmptyAndInvalid(t *testing.T) {
	t.Parallel()
	base := []string{"A=1", "noeq"}
	if got := mergeShellEnv(base, nil); len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if got := mergeShellEnv(base, []string{}); len(got) != 2 {
		t.Fatalf("%v", got)
	}
	out := mergeShellEnv(base, []string{"=novalue", "B=2", "A=3"})
	m := map[string]string{}
	for _, kv := range out {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			m[k] = v
		}
	}
	if m["A"] != "3" || m["B"] != "2" {
		t.Fatalf("%v", out)
	}
}

func TestShellEnvFromQueryEdges(t *testing.T) {
	t.Parallel()
	// empty map
	if shellEnvFromQuery(nil) != nil || shellEnvFromQuery(map[string]string{}) != nil {
		t.Fatal("empty")
	}
	// control char reject + length cap
	long := strings.Repeat("a", 600)
	got := shellEnvFromQuery(map[string]string{
		"term":  long,
		"lang":  "x\x00y",
		"colorterm": "ok",
	})
	join := strings.Join(got, "\n")
	if !strings.Contains(join, "COLORTERM=ok") {
		t.Fatalf("%v", got)
	}
	for _, kv := range got {
		if strings.HasPrefix(kv, "TERM=") && len(kv) > 5+512 {
			t.Fatalf("term not capped: %d", len(kv))
		}
		if strings.HasPrefix(kv, "LANG=") {
			t.Fatal("null should reject")
		}
	}
}

func TestShellEnvToQueryEdges(t *testing.T) {
	t.Parallel()
	q := make(url.Values)
	shellEnvToQuery(q, nil)
	shellEnvToQuery(q, []string{})
	shellEnvToQuery(q, []string{
		"not-a-pair",
		"EMPTY=",
		"term=xterm-lower", // case-insensitive env name
		"UNKNOWN=skip",
	})
	if q.Get("term") != "xterm-lower" {
		t.Fatalf("%v", q)
	}
	if q.Get("unknown") != "" {
		t.Fatal("unknown should skip")
	}
}

func TestMergeShellEnvEdges(t *testing.T) {
	t.Parallel()
	base := []string{"A=1", "noequals", "B=2"}
	if got := mergeShellEnv(base, nil); len(got) != len(base) {
		t.Fatal("nil override")
	}
	if got := mergeShellEnv(base, []string{}); len(got) != len(base) {
		t.Fatal("empty override")
	}
	out := mergeShellEnv(base, []string{"=novalkey", "C=3", "A=9"})
	m := map[string]string{}
	for _, kv := range out {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			m[k] = v
		}
	}
	if m["A"] != "9" || m["C"] != "3" || m["B"] != "2" {
		t.Fatalf("%v", out)
	}
}

package main

import (
	"strings"
	"testing"
)

func TestRequiredMCPToolsNonEmpty(t *testing.T) {
	t.Parallel()
	need := requiredMCPTools()
	if len(need) < 15 {
		t.Fatalf("need more tools: %d", len(need))
	}
	for _, n := range need {
		if !strings.HasPrefix(n, "grain_") {
			t.Fatalf("unexpected %q", n)
		}
	}
}

func TestCollectAndMissing(t *testing.T) {
	t.Parallel()
	tools := []struct {
		Name        string
		Description string
	}{
		{Name: "grain_health"},
		{Name: "grain_exec"},
	}
	names := collectToolNames(tools)
	if len(names) != 2 {
		t.Fatal(names)
	}
	miss := missingRequired(names, requiredMCPTools())
	if len(miss) == 0 {
		t.Fatal("expected missing")
	}
	msg := reportMissing(miss)
	if !strings.Contains(msg, "missing") {
		t.Fatal(msg)
	}
	// full set → no missing
	full := requiredMCPTools()
	if m := missingRequired(full, full); len(m) != 0 {
		t.Fatal(m)
	}
}

func TestFormatToolsJSON(t *testing.T) {
	t.Parallel()
	s, err := formatToolsJSON([]string{"grain_health", "grain_exec"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, `"count": 2`) && !strings.Contains(s, `"count":2`) {
		t.Fatal(s)
	}
	if !strings.Contains(s, "grain_health") {
		t.Fatal(s)
	}
}

func TestPickGrainBin(t *testing.T) {
	t.Parallel()
	if pickGrainBin([]string{"mcp-handshake"}) != "./bin/grain" {
		t.Fatal(pickGrainBin([]string{"mcp-handshake"}))
	}
	if pickGrainBin([]string{"mcp-handshake", "/x/grain"}) != "/x/grain" {
		t.Fatal(pickGrainBin([]string{"mcp-handshake", "/x/grain"}))
	}
	if pickGrainBin([]string{"mcp-handshake", "  "}) != "./bin/grain" {
		t.Fatal("whitespace arg should fall back")
	}
}

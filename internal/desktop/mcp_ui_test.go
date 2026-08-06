package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetMCPEnabledAndStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: qemu\napi: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetMCPEnabled(path, true, "127.0.0.1:17476"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "mcp:") || !strings.Contains(string(b), "enabled: true") {
		t.Fatalf("config missing mcp: %s", b)
	}
	cfg := Defaults()
	st, err := GetMCPStatus(path, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled {
		t.Fatal("want enabled")
	}
	if st.Listen != "127.0.0.1:17476" {
		t.Fatalf("listen %q", st.Listen)
	}
	if st.CursorSnippet == "" || st.ClaudeSnippet == "" || st.GenericSnippet == "" {
		t.Fatal("snippets empty")
	}
	if !strings.Contains(st.CursorSnippet, "17476") {
		t.Fatalf("cursor snippet: %s", st.CursorSnippet)
	}

	stR, err := GetMCPStatus(path, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if stR.Local {
		t.Fatal("remote should not be local")
	}
	if !strings.Contains(stR.Message, "Remote") {
		t.Fatalf("message %q", stR.Message)
	}
}

func TestNormalizeListen(t *testing.T) {
	if normalizeListen(":7476") != "127.0.0.1:7476" {
		t.Fatal(normalizeListen(":7476"))
	}
	if normalizeListen("") != "127.0.0.1:7476" {
		t.Fatal(normalizeListen(""))
	}
}

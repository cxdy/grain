package desktop

import (
	"net"
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
	if normalizeListen("127.0.0.1:9/mcp") != "127.0.0.1:9" {
		t.Fatal(normalizeListen("127.0.0.1:9/mcp"))
	}
}

func TestGetMCPStatusDisabledAndListening(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// no mcp section → defaults disabled-ish (enabled false)
	if err := os.WriteFile(path, []byte("api: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := GetMCPStatus(path, Defaults(), true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled {
		t.Fatal("want disabled by default")
	}
	if !strings.Contains(st.Message, "disabled") {
		t.Fatalf("%q", st.Message)
	}

	// enable and listen on real TCP
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().String()
	if err := SetMCPEnabled(path, true, addr); err != nil {
		t.Fatal(err)
	}
	st2, err := GetMCPStatus(path, Defaults(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.Enabled || !st2.Listening {
		t.Fatalf("%+v", st2)
	}
	if !strings.Contains(st2.Message, "listening") {
		t.Fatalf("%q", st2.Message)
	}

	// enabled but not listening
	if err := SetMCPEnabled(path, true, "127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	st3, err := GetMCPStatus(path, Defaults(), true)
	if err != nil {
		t.Fatal(err)
	}
	if st3.Listening {
		t.Fatal("want not listening")
	}

	// listen with scheme → "configured" branch
	// write mcp with listen containing ://
	if err := os.WriteFile(path, []byte("mcp:\n  enabled: true\n  listen: http://127.0.0.1:7476\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st4, err := GetMCPStatus(path, Defaults(), true)
	if err != nil {
		t.Fatal(err)
	}
	if st4.Message != "configured" {
		t.Fatalf("%q", st4.Message)
	}

	// invalid yaml
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte(":\n:"), 0o600)
	if _, err := GetMCPStatus(bad, Defaults(), true); err == nil {
		t.Fatal("want parse error")
	}

	// empty path + missing home config is ok
	if _, err := GetMCPStatus("", Defaults(), false); err != nil {
		t.Fatal(err)
	}
}

func TestReadMCPFromFileVariants(t *testing.T) {
	dir := t.TempDir()
	// mcp non-map
	p := filepath.Join(dir, "a.yaml")
	_ = os.WriteFile(p, []byte("mcp: true\n"), 0o600)
	en, listen, err := readMCPFromFile(p)
	if err != nil || en || listen == "" {
		t.Fatalf("%v %v %q", en, listen, err)
	}
	// mcp with empty listen
	p2 := filepath.Join(dir, "b.yaml")
	_ = os.WriteFile(p2, []byte("mcp:\n  enabled: true\n"), 0o600)
	en, listen, err = readMCPFromFile(p2)
	if err != nil || !en || listen != "127.0.0.1:7476" {
		t.Fatalf("%v %q %v", en, listen, err)
	}
	// missing
	if _, _, err := readMCPFromFile(filepath.Join(dir, "nope")); err == nil {
		t.Fatal("want missing")
	}
}

func TestSetMCPEnabledDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "c.yaml")
	if err := SetMCPEnabled(path, false, ""); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "enabled: false") || !strings.Contains(string(b), "127.0.0.1:7476") {
		t.Fatalf("%s", b)
	}
	// invalid yaml existing
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte(":\n:"), 0o600)
	if err := SetMCPEnabled(bad, true, "x"); err == nil {
		t.Fatal("want parse")
	}
}

func TestMCPSnippets(t *testing.T) {
	if !strings.Contains(mcpCursorSnippet(":9"), "127.0.0.1:9") {
		t.Fatal(mcpCursorSnippet(":9"))
	}
	if !strings.Contains(mcpClaudeSnippet("1.2.3.4:5"), "http://1.2.3.4:5/mcp") {
		t.Fatal(mcpClaudeSnippet("1.2.3.4:5"))
	}
	if !strings.Contains(mcpGenericSnippet(""), "7476") {
		t.Fatal(mcpGenericSnippet(""))
	}
}

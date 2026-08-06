package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveHostConnection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("cpus: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SaveHostConnection(path, ConnectionWithMCP{
		Name:       "lab",
		API:        "http://10.0.0.5:7474",
		Token:      "secret",
		MCPEnabled: true,
		MCPListen:  "10.0.0.5:7476",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "lab") || !strings.Contains(string(b), "10.0.0.5:7474") {
		t.Fatalf("%s", b)
	}
	// update same name
	err = SaveHostConnection(path, ConnectionWithMCP{
		Name: "lab",
		API:  "http://10.0.0.5:7475",
		Token: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSaveHostConnectionValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := SaveHostConnection(path, ConnectionWithMCP{}); err == nil {
		t.Fatal("want name error")
	}
	if err := SaveHostConnection(path, ConnectionWithMCP{Name: "local", API: "http://x"}); err == nil {
		t.Fatal("local reserved")
	}
	if err := SaveHostConnection(path, ConnectionWithMCP{Name: "a", API: ""}); err == nil {
		t.Fatal("api required")
	}
	if err := SaveHostConnection(path, ConnectionWithMCP{Name: "a", API: "http://x", MCPEnabled: true}); err == nil {
		t.Fatal("mcp listen required")
	}
}

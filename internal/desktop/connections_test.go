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

func TestDeleteConnection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := SaveHostConnection(path, ConnectionWithMCP{Name: "lab", API: "http://127.0.0.1:7474"}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConnection(path, "local"); err == nil {
		t.Fatal("cannot delete local")
	}
	if err := DeleteConnection(path, "lab"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "lab") {
		t.Fatalf("lab still present: %s", b)
	}
	if err := DeleteConnection(path, "missing"); err == nil {
		t.Fatal("want missing error")
	}
}

func TestSaveSettingsForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: qemu\napi: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SaveSettingsForm(path, SettingsForm{
		DefaultConnection: "local",
		StartLocalDaemon:  false,
		DataDir:           dir + "/grain-data",
		API:               "127.0.0.1:7475",
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "start_local_daemon: false") {
		t.Fatalf("start_local_daemon: %s", s)
	}
	if !strings.Contains(s, "default_connection: local") {
		t.Fatalf("default_connection: %s", s)
	}
	if !strings.Contains(s, "7475") {
		t.Fatalf("api: %s", s)
	}
	if !strings.HasSuffix(s, "\n") {
		t.Fatal("want trailing newline")
	}
	if !strings.Contains(s, "hypervisor") {
		t.Fatalf("lost hypervisor: %s", s)
	}
}

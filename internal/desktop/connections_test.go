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
		Name:  "lab",
		API:   "http://10.0.0.5:7475",
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
	if err := AddConnection(path, Connection{Name: ""}); err == nil {
		t.Fatal("want name required")
	}
	if err := AddConnection(path, Connection{Name: "local"}); err == nil {
		t.Fatal("want local reserved")
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

func TestAddConnectionFullFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := AddConnection(path, Connection{
		Name: "lab", API: "10.0.0.1:7474", Token: "t", TokenEnv: "LAB", Notes: "mcp:1.2.3.4:9",
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, "token_env") || !strings.Contains(s, "notes") || !strings.Contains(s, "http://") {
		t.Fatalf("%s", s)
	}
	// invalid yaml
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte(":\n:"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddConnection(bad, Connection{Name: "x", API: "http://x"}); err == nil {
		t.Fatal("want parse error")
	}
	// create from missing file
	path2 := filepath.Join(dir, "new", "c.yaml")
	if err := AddConnection(path2, Connection{Name: "a", API: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	// non-map connection entries skipped on update
	if err := os.WriteFile(path, []byte("connections:\n  - notamap\n  - name: lab\n    api: http://x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddConnection(path, Connection{Name: "lab", API: "http://y"}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteConnectionResetsDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
connections:
  - name: lab
    api: http://10.0.0.5:7474
  - name: other
    api: http://10.0.0.6:7474
desktop:
  default_connection: lab
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConnection(path, "lab"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "default_connection: local") {
		t.Fatalf("want default reset: %s", b)
	}
	if strings.Contains(string(b), "name: lab") {
		t.Fatalf("lab still there: %s", b)
	}
	// empty name
	if err := DeleteConnection(path, "  "); err == nil {
		t.Fatal("want empty name error")
	}
	// invalid yaml
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte(":\n:"), 0o600)
	if err := DeleteConnection(bad, "x"); err == nil {
		t.Fatal("want parse")
	}
	// missing file
	if err := DeleteConnection(filepath.Join(dir, "nope.yaml"), "x"); err == nil {
		t.Fatal("want missing file")
	}
}

func TestSaveSettingsFormExtras(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	// missing file creates
	if err := SaveSettingsForm(path, SettingsForm{
		APIURL: "10.0.0.9:7474",
		// empty default → local
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, "api_url") || !strings.Contains(s, "default_connection: local") {
		t.Fatalf("%s", s)
	}
	// invalid yaml
	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte(":\n:"), 0o600)
	if err := SaveSettingsForm(bad, SettingsForm{}); err == nil {
		t.Fatal("want parse")
	}
}

func TestDeleteConnectionNonMapEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
connections:
  - just-a-string
  - name: lab
    api: http://10.0.0.5:7474
  - 42
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConnection(path, "lab"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Contains(s, "name: lab") {
		t.Fatalf("lab still present: %s", s)
	}
	// non-map entries preserved
	if !strings.Contains(s, "just-a-string") {
		t.Fatalf("lost non-map entry: %s", s)
	}
}

func TestSaveSettingsFormMkdirFail(t *testing.T) {
	dir := t.TempDir()
	// parent path is a file so MkdirAll fails
	parent := filepath.Join(dir, "notdir")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettingsForm(filepath.Join(parent, "cfg.yaml"), SettingsForm{API: "127.0.0.1:1"}); err == nil {
		t.Fatal("want mkdir error")
	}
	// AddConnection mkdir fail
	if err := AddConnection(filepath.Join(parent, "c.yaml"), Connection{Name: "x", API: "http://x"}); err == nil {
		t.Fatal("want mkdir error")
	}
}

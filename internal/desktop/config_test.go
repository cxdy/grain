package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.yaml")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Socket == "" || cfg.DataDir == "" {
		t.Fatalf("defaults: %+v", cfg)
	}
	if len(cfg.Connections) < 1 || cfg.Connections[0].Name != "local" {
		t.Fatalf("connections: %+v", cfg.Connections)
	}
	if !cfg.Desktop.StartLocalDaemonEnabled() {
		t.Fatal("start local default true")
	}
}

func TestLoadConfigConnections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
data_dir: ` + dir + `
socket: ` + filepath.Join(dir, "grain.sock") + `
api_token: secret
cpus: 4
memory_mb: 4096
image: grain-ubuntu
connections:
  - name: lab
    api: 10.0.0.5:7474
    token_env: LAB_TOKEN
desktop:
  default_connection: lab
  start_local_daemon: false
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ResolvedAPIToken() != "secret" {
		t.Fatalf("token: %q", cfg.ResolvedAPIToken())
	}
	if cfg.DefaultCPUs != 4 || cfg.DefaultMemoryMB != 4096 {
		t.Fatalf("resources: %+v", cfg)
	}
	if cfg.Desktop.DefaultConnection != "lab" {
		t.Fatalf("default conn: %q", cfg.Desktop.DefaultConnection)
	}
	if cfg.Desktop.StartLocalDaemonEnabled() {
		t.Fatal("start_local_daemon false")
	}
	// local prepended
	names := ConnectionNames(cfg.ActiveConnections())
	if len(names) < 2 || names[0] != "local" {
		t.Fatalf("names: %v", names)
	}
	lab, err := cfg.ConnectionByName("lab")
	if err != nil {
		t.Fatal(err)
	}
	if lab.API != "http://10.0.0.5:7474" {
		t.Fatalf("lab api: %q", lab.API)
	}
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte(":\n:"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("want parse error")
	}
}

func TestLoadConfigEmptyPathUsesDefaultLocation(t *testing.T) {
	// empty path uses Defaults().DataDir/config.yaml — may or may not exist
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir == "" {
		t.Fatal("expected data dir")
	}
}

func TestAuthTokenFallback(t *testing.T) {
	t.Parallel()
	c := Config{AuthToken: "a"}
	if c.ResolvedAPIToken() != "a" {
		t.Fatal(c.ResolvedAPIToken())
	}
	c.APIToken = "b"
	if c.ResolvedAPIToken() != "b" {
		t.Fatal(c.ResolvedAPIToken())
	}
}

func TestDesktopPrefsNilPointer(t *testing.T) {
	t.Parallel()
	var p DesktopPrefs
	if !p.StartLocalDaemonEnabled() {
		t.Fatal("nil means true")
	}
	f := false
	p.StartLocalDaemon = &f
	if p.StartLocalDaemonEnabled() {
		t.Fatal("false")
	}
}

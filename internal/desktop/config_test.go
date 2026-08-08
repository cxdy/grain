package desktop

import (
	"os"
	"path/filepath"
	"strings"
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
	tr := true
	p.StartLocalDaemon = &tr
	if !p.StartLocalDaemonEnabled() {
		t.Fatal("true")
	}
}

func TestLoadConfigApplyDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// minimal yaml forces applyDefaults for empty fields
	yaml := `
api_url: 10.0.0.5:7474
auth_token: legacy
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultCPUs <= 0 || cfg.DefaultMemoryMB <= 0 || cfg.DefaultDiskGB <= 0 {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.Image == "" {
		t.Fatal("image default")
	}
	if cfg.ResolvedAPIToken() != "legacy" {
		t.Fatalf("auth token: %q", cfg.ResolvedAPIToken())
	}
	if !strings.HasPrefix(cfg.APIURL, "http://") {
		t.Fatalf("api_url normalize: %q", cfg.APIURL)
	}
	if cfg.Desktop.DefaultConnection != "local" {
		t.Fatalf("default conn %q", cfg.Desktop.DefaultConnection)
	}
	// socket under data_dir when omitted
	if cfg.Socket == "" {
		t.Fatal("socket")
	}
	// tilde expand paths
	path2 := filepath.Join(dir, "tilde.yaml")
	if err := os.WriteFile(path2, []byte("data_dir: ~/grain-test-data\nsocket: ~/grain-test-data/s.sock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg2, err := LoadConfig(path2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(cfg2.DataDir, "~") {
		t.Fatalf("want expanded data_dir: %q", cfg2.DataDir)
	}
}

func TestLoadConfigExplicitEmptyFields(t *testing.T) {
	// YAML that clears defaults so applyDefaults fills every empty branch.
	dir := t.TempDir()
	path := filepath.Join(dir, "empty-fields.yaml")
	yaml := `
data_dir: ""
socket: ""
cpus: 0
memory_mb: 0
disk_gb: 0
image: ""
desktop:
  default_connection: ""
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir == "" || cfg.Socket == "" || cfg.Image == "" {
		t.Fatalf("applyDefaults incomplete: %+v", cfg)
	}
	if cfg.DefaultCPUs <= 0 || cfg.DefaultMemoryMB <= 0 || cfg.DefaultDiskGB <= 0 {
		t.Fatalf("resource defaults: %+v", cfg)
	}
	if cfg.Desktop.DefaultConnection != "local" {
		t.Fatalf("default connection: %q", cfg.Desktop.DefaultConnection)
	}
}

func TestLoadConfigReadPermissionError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "no-read.yaml")
	if err := os.WriteFile(path, []byte("cpus: 2\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(path, 0o600) }()
	_, err := LoadConfig(path)
	// On some platforms root/CI can still read mode 000; tolerate either.
	if err != nil && !strings.Contains(err.Error(), "read config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigActiveConnectionsAndByName(t *testing.T) {
	cfg := Defaults()
	cfg.Connections = []Connection{{Name: "lab", API: "http://x"}}
	list := cfg.ActiveConnections()
	if list[0].Name != "local" {
		t.Fatalf("%+v", list)
	}
	c, err := cfg.ConnectionByName("lab")
	if err != nil || c.Name != "lab" {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := cfg.ConnectionByName("nope"); err == nil {
		t.Fatal("want missing")
	}
	// empty name uses default
	c, err = cfg.ConnectionByName("")
	if err != nil || c.Name != "local" {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestApplyDefaultsAllBranches(t *testing.T) {
	t.Parallel()
	cfg := Config{
		DataDir:         "~/grain-data",
		Socket:          "",
		DefaultCPUs:     0,
		DefaultMemoryMB: -1,
		DefaultDiskGB:   0,
		Image:           "",
		APIURL:          "10.1.2.3:7474",
		Desktop:         DesktopPrefs{DefaultConnection: ""},
	}
	cfg.applyDefaults()
	if !strings.Contains(cfg.DataDir, "grain-data") {
		t.Fatalf("data dir expand: %q", cfg.DataDir)
	}
	if cfg.Socket == "" || cfg.DefaultCPUs <= 0 || cfg.DefaultMemoryMB <= 0 || cfg.DefaultDiskGB <= 0 {
		t.Fatalf("defaults: %+v", cfg)
	}
	if cfg.Image == "" {
		t.Fatal("image default")
	}
	if cfg.Desktop.DefaultConnection != "local" {
		t.Fatal(cfg.Desktop.DefaultConnection)
	}
	if !strings.HasPrefix(cfg.APIURL, "http://") {
		t.Fatalf("api url: %q", cfg.APIURL)
	}
	// already set fields preserved
	cfg2 := Config{DataDir: t.TempDir(), Socket: "/tmp/x.sock", DefaultCPUs: 8, DefaultMemoryMB: 8192, DefaultDiskGB: 40, Image: "custom", Desktop: DesktopPrefs{DefaultConnection: "lab"}}
	cfg2.applyDefaults()
	if cfg2.DefaultCPUs != 8 || cfg2.Image != "custom" || cfg2.Desktop.DefaultConnection != "lab" {
		t.Fatalf("%+v", cfg2)
	}
}

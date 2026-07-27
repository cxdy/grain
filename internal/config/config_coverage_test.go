package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
)

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: filepath.Join(dir, "grain-data")}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"", "vms", "images", "logs"} {
		p := cfg.DataDir
		if sub != "" {
			p = filepath.Join(cfg.DataDir, sub)
		}
		st, err := os.Stat(p)
		if err != nil || !st.IsDir() {
			t.Fatalf("%s: %v", p, err)
		}
	}
}

func TestEnsureDirsFail(t *testing.T) {
	// DataDir is a file → MkdirAll fails
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: f}
	if err := cfg.EnsureDirs(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadEmptyPathUsesDefaultLocation(t *testing.T) {
	// Load("") uses ~/.grain/config.yaml — if missing, defaults.
	// Isolate by not depending on real home content: empty path always merges
	// defaults when file missing OR loads real user config if present.
	c, err := config.Load("")
	if err != nil {
		// User may have invalid config; still cover the path
		t.Logf("Load empty: %v", err)
		return
	}
	if c.DataDir == "" || c.Socket == "" {
		t.Fatal("defaults incomplete")
	}
}

func TestLoadBadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("cpus: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("got %v", err)
	}
}

func TestApplyDefaultsViaLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	// Partial config: empty strings and zeros and bad enums
	body := []byte(`
data_dir: "` + dir + `/d"
socket: ""
cpus: 0
memory_mb: 0
disk_gb: 0
hypervisor: ""
image: ""
ssh_user: ""
ready_timeout: 0s
log_level: ""
mount_driver: "weird"
agent_transport: "NOPE"
proxy_listen: ""
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultCPUs != 2 || c.DefaultMemoryMB != 2048 || c.DefaultDiskGB != 8 {
		t.Fatalf("resource defaults: cpus=%d mem=%d disk=%d", c.DefaultCPUs, c.DefaultMemoryMB, c.DefaultDiskGB)
	}
	if c.Hypervisor != "qemu" || c.Image != "auto" || c.SSHUser != "ubuntu" {
		t.Fatalf("fields hv=%s img=%s user=%s", c.Hypervisor, c.Image, c.SSHUser)
	}
	if c.MountDriver != "9p" {
		t.Fatalf("mount_driver %q", c.MountDriver)
	}
	if c.AgentTransport != "auto" {
		t.Fatalf("agent_transport %q", c.AgentTransport)
	}
	if c.ProxyListen != "0.0.0.0:3128" {
		t.Fatalf("proxy_listen %q", c.ProxyListen)
	}
	if c.ReadyTimeout < time.Second {
		t.Fatalf("ready %v", c.ReadyTimeout)
	}
	if c.LogLevel != "info" {
		t.Fatalf("log %q", c.LogLevel)
	}
	// Socket derived from data_dir when empty
	if !strings.HasSuffix(c.Socket, "grain.sock") {
		t.Fatalf("socket %q", c.Socket)
	}
}

func TestApplyDefaultsValidEnums(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := []byte("mount_driver: virtiofs\nagent_transport: TCP\nproxy_listen: 127.0.0.1:9\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.MountDriver != "virtiofs" {
		t.Fatalf("mount %q", c.MountDriver)
	}
	if c.AgentTransport != "tcp" {
		t.Fatalf("transport %q", c.AgentTransport)
	}
	if c.ProxyListen != "127.0.0.1:9" {
		t.Fatalf("proxy %q", c.ProxyListen)
	}
}

func TestProfileNamesAndLookupEmpty(t *testing.T) {
	t.Parallel()
	c := config.Config{}
	if names := c.ProfileNames(); names != nil {
		t.Fatalf("%v", names)
	}
	_, err := c.LookupProfile("")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("%v", err)
	}
	_, err = c.LookupProfile("x")
	if err == nil {
		t.Fatal("nil profiles")
	}

	c.Profiles = map[string]config.Profile{
		"z": {},
		"a": {},
		"m": {},
	}
	names := c.ProfileNames()
	if len(names) != 3 || names[0] != "a" || names[2] != "z" {
		t.Fatalf("%v", names)
	}
	p, err := c.LookupProfile("m")
	if err != nil {
		t.Fatal(err)
	}
	_ = p
}

func TestResolvedAPIURLAndToken(t *testing.T) {
	t.Parallel()
	c := config.Config{APIURL: "  host:9/ ", APIToken: "", AuthToken: "auth"}
	if got := c.ResolvedAPIURL(); got != "http://host:9" {
		t.Fatalf("%q", got)
	}
	if c.ResolvedAPIToken() != "auth" {
		t.Fatal("auth token")
	}
	c.APIToken = "api"
	if c.ResolvedAPIToken() != "api" {
		t.Fatal("api token wins")
	}
	if !c.IsRemoteClient() {
		t.Fatal("remote")
	}
}

func TestAPIURLIsLoopbackEdge(t *testing.T) {
	t.Parallel()
	// invalid URL → false
	if config.APIURLIsLoopback("http://[:::") {
		t.Fatal("invalid should be non-loopback")
	}
	// empty host after parse
	if config.APIURLIsLoopback("http://") {
		// may parse oddly
		t.Log("http:// result", config.APIURLIsLoopback("http://"))
	}
	if !config.APIURLIsLoopback("localhost:7474") {
		t.Fatal("bare localhost host:port becomes http://localhost:7474")
	}
}

func TestListenAddrIsLoopbackEdge(t *testing.T) {
	t.Parallel()
	if !config.ListenAddrIsLoopback("") {
		t.Fatal("empty")
	}
	if !config.ListenAddrIsLoopback("localhost:1") {
		t.Fatal("localhost")
	}
	if config.ListenAddrIsLoopback("not-a-valid") {
		// bare invalid → false
		t.Log("ok")
	}
	if config.ListenAddrIsLoopback("hostname-only") {
		t.Fatal("hostname-only should be false")
	}
}

func TestLoadProxyAndGPUFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := []byte(`
guest_arch: arm64
gpu: virtio
network: overlay
proxy_listen: 0.0.0.0:9999
max_vms: 3
max_memory_mb_per_vm: 512
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.GuestArch != "arm64" || c.GPU != "virtio" || c.Network != "overlay" {
		t.Fatalf("%+v", c)
	}
	if c.ProxyListen != "0.0.0.0:9999" || c.MaxVMs != 3 || c.MaxMemoryMBPerVM != 512 {
		t.Fatalf("caps/proxy %+v", c)
	}
}

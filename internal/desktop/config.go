package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DesktopPrefs are UI/runtime preferences under config.yaml `desktop:`.
type DesktopPrefs struct {
	// DefaultConnection is the profile name used on launch (default "local").
	DefaultConnection string `yaml:"default_connection" json:"default_connection"`
	// StartLocalDaemon enables splash auto-start of the local daemon (default true).
	StartLocalDaemon *bool `yaml:"start_local_daemon" json:"start_local_daemon"`
	// RunMCPIfEnabled is reserved for later MCP ensure-running (default true).
	RunMCPIfEnabled *bool `yaml:"run_mcp_if_enabled" json:"run_mcp_if_enabled"`
}

// StartLocalDaemonEnabled returns whether splash should start the local daemon.
func (p DesktopPrefs) StartLocalDaemonEnabled() bool {
	if p.StartLocalDaemon == nil {
		return true
	}
	return *p.StartLocalDaemon
}

// Config is the Desktop-relevant subset of ~/.grain/config.yaml.
type Config struct {
	DataDir     string       `yaml:"data_dir"`
	Socket      string       `yaml:"socket"`
	API         string       `yaml:"api"` // daemon listen (not client dial target)
	APIURL      string       `yaml:"api_url"`
	APIToken    string       `yaml:"api_token"`
	AuthToken   string       `yaml:"auth_token"`
	Connections []Connection `yaml:"connections"`
	Desktop     DesktopPrefs `yaml:"desktop"`
	// Default create knobs (mirrored for create form defaults).
	DefaultCPUs     int    `yaml:"cpus"`
	DefaultMemoryMB int    `yaml:"memory_mb"`
	DefaultDiskGB   int    `yaml:"disk_gb"`
	Image           string `yaml:"image"`
}

// ResolvedAPIToken returns api_token or auth_token.
func (c Config) ResolvedAPIToken() string {
	if strings.TrimSpace(c.APIToken) != "" {
		return strings.TrimSpace(c.APIToken)
	}
	return strings.TrimSpace(c.AuthToken)
}

// Defaults returns developer-friendly local defaults.
func Defaults() Config {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	dir := filepath.Join(home, ".grain")
	return Config{
		DataDir:         dir,
		Socket:          filepath.Join(dir, "grain.sock"),
		API:             "127.0.0.1:7474",
		DefaultCPUs:     2,
		DefaultMemoryMB: 2048,
		DefaultDiskGB:   8,
		Image:           "grain-ubuntu",
		Desktop: DesktopPrefs{
			DefaultConnection: "local",
		},
	}
}

// LoadConfig reads YAML from path (or default ~/.grain/config.yaml).
// Missing file returns Defaults without error.
func LoadConfig(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		path = filepath.Join(cfg.DataDir, "config.yaml")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.applyDefaults()
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	d := Defaults()
	if c.DataDir == "" {
		c.DataDir = d.DataDir
	}
	c.DataDir = expandHome(c.DataDir)
	if c.Socket == "" {
		c.Socket = filepath.Join(c.DataDir, "grain.sock")
	}
	c.Socket = expandHome(c.Socket)
	if c.DefaultCPUs <= 0 {
		c.DefaultCPUs = d.DefaultCPUs
	}
	if c.DefaultMemoryMB <= 0 {
		c.DefaultMemoryMB = d.DefaultMemoryMB
	}
	if c.DefaultDiskGB <= 0 {
		c.DefaultDiskGB = d.DefaultDiskGB
	}
	if c.Image == "" {
		c.Image = d.Image
	}
	if c.Desktop.DefaultConnection == "" {
		c.Desktop.DefaultConnection = "local"
	}
	// Seed local connection list
	c.Connections = EnsureLocalConnection(c.Connections, c.Socket, c.DataDir)
	// Legacy api_url as implicit remote? Keep separate; connections are primary.
	if c.APIURL != "" {
		c.APIURL = NormalizeAPIURL(c.APIURL)
	}
}

// ActiveConnections returns the resolved connection list.
func (c Config) ActiveConnections() []Connection {
	return EnsureLocalConnection(c.Connections, c.Socket, c.DataDir)
}

// ConnectionByName resolves a profile by name.
func (c Config) ConnectionByName(name string) (Connection, error) {
	return ResolveConnection(c.ActiveConnections(), name, c.Desktop.DefaultConnection, c.Socket, c.DataDir)
}

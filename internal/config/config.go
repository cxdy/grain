package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is daemon + CLI shared configuration.
// Keep fields short and obvious — this is the whole knobs surface for v0.1.
type Config struct {
	// DataDir holds disks, keys, state. Default: ~/.grain
	DataDir string `yaml:"data_dir"`
	// Socket is the daemon unix socket path.
	Socket string `yaml:"socket"`
	// API is optional TCP bind for metrics/API (empty = unix only).
	API string `yaml:"api"`
	// MetricsAddr is Prometheus scrape address (empty = disabled).
	MetricsAddr string `yaml:"metrics_addr"`
	// DefaultCPUs for new sandboxes.
	DefaultCPUs int `yaml:"cpus"`
	// DefaultMemoryMB for new sandboxes.
	DefaultMemoryMB int `yaml:"memory_mb"`
	// DefaultDiskGB root disk size for new VMs.
	DefaultDiskGB int `yaml:"disk_gb"`
	// Hypervisor: qemu | mock
	Hypervisor string `yaml:"hypervisor"`
	// QEMUBinary override (default: qemu-system-aarch64 or qemu-system-x86_64).
	QEMUBinary string `yaml:"qemu_binary"`
	// Image is the base image id (kernel+disk set under DataDir/images).
	Image string `yaml:"image"`
	// SSHUser for guest access (cloud images).
	SSHUser string `yaml:"ssh_user"`
	// ReadyTimeout waits for SSH after boot.
	ReadyTimeout time.Duration `yaml:"ready_timeout"`
	// LogLevel: debug|info|warn|error
	LogLevel string `yaml:"log_level"`

	// Resource caps (0 = unlimited). Stopped VMs do not count toward running caps.
	// MaxVMs is the maximum number of concurrently running/creating VMs.
	MaxVMs int `yaml:"max_vms"`
	// MaxCPUsTotal is the sum of CPUs across running/creating VMs.
	MaxCPUsTotal int `yaml:"max_cpus_total"`
	// MaxMemoryMBTotal is the sum of MemoryMB across running/creating VMs.
	MaxMemoryMBTotal int `yaml:"max_memory_mb_total"`
	// MaxCPUsPerVM rejects a single VM requesting more CPUs than this.
	MaxCPUsPerVM int `yaml:"max_cpus_per_vm"`
	// MaxMemoryMBPerVM rejects a single VM requesting more MemoryMB than this.
	MaxMemoryMBPerVM int `yaml:"max_memory_mb_per_vm"`
}

// Defaults returns developer-friendly defaults for local Mac/Linux use.
func Defaults() Config {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	dir := filepath.Join(home, ".grain")
	return Config{
		DataDir:          dir,
		Socket:           filepath.Join(dir, "grain.sock"),
		API:              "127.0.0.1:7474",
		MetricsAddr:      "127.0.0.1:9091",
		DefaultCPUs:      2,
		DefaultMemoryMB:  2048,
		DefaultDiskGB:    8,
		Hypervisor:       "qemu",
		Image:            "ubuntu-cloud",
		SSHUser:          "ubuntu",
		ReadyTimeout:     120 * time.Second,
		LogLevel:         "info",
		MaxVMs:           8,
		MaxCPUsTotal:     16,
		MaxMemoryMBTotal: 32768,
		MaxCPUsPerVM:     8,
		MaxMemoryMBPerVM: 16384,
	}
}

// Load reads YAML from path, or returns Defaults if path is empty/missing.
func Load(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		// optional config file next to data dir
		path = filepath.Join(cfg.DataDir, "config.yaml")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
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
	if c.Socket == "" {
		c.Socket = filepath.Join(c.DataDir, "grain.sock")
	}
	if c.DefaultCPUs <= 0 {
		c.DefaultCPUs = d.DefaultCPUs
	}
	if c.DefaultMemoryMB <= 0 {
		c.DefaultMemoryMB = d.DefaultMemoryMB
	}
	if c.DefaultDiskGB <= 0 {
		c.DefaultDiskGB = d.DefaultDiskGB
	}
	if c.Hypervisor == "" {
		c.Hypervisor = d.Hypervisor
	}
	if c.Image == "" {
		c.Image = d.Image
	}
	if c.SSHUser == "" {
		c.SSHUser = d.SSHUser
	}
	if c.ReadyTimeout <= 0 {
		c.ReadyTimeout = d.ReadyTimeout
	}
	if c.LogLevel == "" {
		c.LogLevel = d.LogLevel
	}
}

// EnsureDirs creates data directories.
func (c Config) EnsureDirs() error {
	for _, p := range []string{
		c.DataDir,
		filepath.Join(c.DataDir, "vms"),
		filepath.Join(c.DataDir, "images"),
		filepath.Join(c.DataDir, "logs"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}

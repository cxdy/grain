package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// APIToken, when non-empty, requires Authorization: Bearer on all daemon
	// routes except GET /healthz. Empty (default) means no auth — suitable for
	// localhost / unix socket. CLI also reads env GRAIN_TOKEN.
	APIToken string `yaml:"api_token"`
	// AuthToken is an alias for APIToken (either field may be set).
	AuthToken string `yaml:"auth_token"`
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
	// MountDriver selects the QEMU shared-fs backend for -v mounts: "9p" (default)
	// or "virtiofs". On darwin, or on Linux without virtiofsd, virtiofs falls
	// back to 9p with a warning. On Linux with virtiofsd available, virtiofs
	// uses vhost-user-fs + a per-mount virtiofsd.
	MountDriver string `yaml:"mount_driver"`
	// AgentTransport selects how the host reaches grain-agent:
	//   auto  — virtio-vsock when /dev/vhost-vsock exists (typical Linux), else TCP hostfwd
	//   tcp   — always SLIRP hostfwd to guest :7475 (default on macOS HVF)
	//   vsock — force vhost-vsock-pci (requires host /dev/vhost-vsock)
	// TCP hostfwd is always configured as a fallback even when vsock is selected.
	AgentTransport string `yaml:"agent_transport"`

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

	// Profiles are named create presets for `grain new --profile NAME`.
	// Resolve order: CLI flags (explicit) > profile fields > global defaults.
	Profiles map[string]Profile `yaml:"profiles"`
}

// Profile is a named set of create-time defaults (see Profiles).
type Profile struct {
	CPUs       int              `yaml:"cpus"`
	MemoryMB   int              `yaml:"memory_mb"`
	DiskGB     int              `yaml:"disk_gb"`
	Image      string           `yaml:"image"`
	Persistent bool             `yaml:"persistent"`
	// Preset is an optional userdata preset name (e.g. docker, k3s).
	Preset   string           `yaml:"preset"`
	Mounts   []ProfileMount   `yaml:"mounts"`
	Forwards []ProfileForward `yaml:"forwards"`
}

// ProfileMount is a host→guest directory share in a profile.
type ProfileMount struct {
	Host  string `yaml:"host"`
	Guest string `yaml:"guest"`
}

// ProfileForward is a host→guest port mapping in a profile.
// HostPort 0 means allocate a free host port.
type ProfileForward struct {
	HostPort  int    `yaml:"host_port"`
	GuestPort int    `yaml:"guest_port"`
	Proto     string `yaml:"proto"`
}

// CreateOverrides are explicitly-set CLI flags for profile resolution.
// Zero-valued ints/strings mean "not set" unless the corresponding *Set bool is true
// (for Persistent, use PersistentSet).
type CreateOverrides struct {
	CPUs          int
	CPUsSet       bool
	MemoryMB      int
	MemoryMBSet   bool
	DiskGB        int
	DiskGBSet     bool
	Image         string
	ImageSet      bool
	Persistent    bool
	PersistentSet bool
	// Preset from CLI --preset (empty + PresetSet false = unset).
	Preset    string
	PresetSet bool
	// When true, CLI -P / -v were provided and override profile lists entirely.
	ForwardsSet bool
	MountsSet   bool
}

// ResolvedCreate is the result of merging CLI flags, a profile, and (later) global defaults.
// Zero CPUs/MemoryMB/DiskGB/Image still mean "use daemon global defaults".
type ResolvedCreate struct {
	CPUs       int
	MemoryMB   int
	DiskGB     int
	Image      string
	Persistent bool
	Preset     string
	Mounts     []ProfileMount
	Forwards   []ProfileForward
	// ProfileName is set when a profile was applied (for Tags["profile"]).
	ProfileName string
}

// LookupProfile returns the named profile or an error if missing.
func (c Config) LookupProfile(name string) (Profile, error) {
	if name == "" {
		return Profile{}, fmt.Errorf("profile name is empty")
	}
	if c.Profiles == nil {
		return Profile{}, fmt.Errorf("unknown profile %q", name)
	}
	p, ok := c.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("unknown profile %q", name)
	}
	return p, nil
}

// ProfileNames returns sorted profile names.
func (c Config) ProfileNames() []string {
	if len(c.Profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ResolveCreate merges CLI overrides with an optional named profile.
// Order: explicit flags > profile fields > leave zero for global defaults (daemon).
// Forwards/mounts: when ForwardsSet/MountsSet, profile lists are not applied
// (caller supplies CLI lists). When unset, profile lists are copied.
func (c Config) ResolveCreate(profileName string, o CreateOverrides) (ResolvedCreate, error) {
	var r ResolvedCreate
	var p Profile
	if profileName != "" {
		var err error
		p, err = c.LookupProfile(profileName)
		if err != nil {
			return r, err
		}
		r.ProfileName = profileName
	}

	// CPUs: flag > profile > 0 (global)
	if o.CPUsSet {
		r.CPUs = o.CPUs
	} else if p.CPUs > 0 {
		r.CPUs = p.CPUs
	}

	if o.MemoryMBSet {
		r.MemoryMB = o.MemoryMB
	} else if p.MemoryMB > 0 {
		r.MemoryMB = p.MemoryMB
	}

	if o.DiskGBSet {
		r.DiskGB = o.DiskGB
	} else if p.DiskGB > 0 {
		r.DiskGB = p.DiskGB
	}

	if o.ImageSet {
		r.Image = o.Image
	} else if p.Image != "" {
		r.Image = p.Image
	}

	if o.PersistentSet {
		r.Persistent = o.Persistent
	} else if profileName != "" {
		r.Persistent = p.Persistent
	}

	if o.PresetSet {
		r.Preset = o.Preset
	} else if p.Preset != "" {
		r.Preset = p.Preset
	}

	if !o.MountsSet && len(p.Mounts) > 0 {
		r.Mounts = append([]ProfileMount(nil), p.Mounts...)
	}
	if !o.ForwardsSet && len(p.Forwards) > 0 {
		r.Forwards = append([]ProfileForward(nil), p.Forwards...)
	}

	return r, nil
}

// ResolvedAPIToken returns api_token, or auth_token if api_token is empty.
func (c Config) ResolvedAPIToken() string {
	if c.APIToken != "" {
		return c.APIToken
	}
	return c.AuthToken
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
		// "auto" prefers a local golden grain-ubuntu when present, else ubuntu-cloud.
		Image:            "auto",
		SSHUser:          "ubuntu",
		ReadyTimeout:     120 * time.Second,
		LogLevel:         "info",
		MountDriver:      "9p",
		AgentTransport:   "auto",
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
	if c.MountDriver == "" {
		c.MountDriver = d.MountDriver
	}
	switch c.MountDriver {
	case "9p", "virtiofs":
		// ok
	default:
		c.MountDriver = "9p"
	}
	if c.AgentTransport == "" {
		c.AgentTransport = d.AgentTransport
	}
	switch strings.ToLower(strings.TrimSpace(c.AgentTransport)) {
	case "auto", "tcp", "vsock":
		c.AgentTransport = strings.ToLower(strings.TrimSpace(c.AgentTransport))
	default:
		c.AgentTransport = "auto"
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

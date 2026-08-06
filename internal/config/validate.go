package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// knownTopLevelKeys are allowed keys in config.yaml (strict check-config).
var knownTopLevelKeys = map[string]struct{}{
	"data_dir": {}, "socket": {}, "api": {}, "api_url": {}, "api_token": {}, "auth_token": {},
	"metrics_addr": {}, "sandbox_metrics_enabled": {}, "sandbox_metrics_interval": {}, "sandbox_metrics_points": {},
	"cpus": {}, "memory_mb": {}, "disk_gb": {},
	"hypervisor": {}, "qemu_binary": {}, "firecracker_binary": {}, "kernel_path": {},
	"image": {}, "ssh_user": {}, "ready_timeout": {}, "log_level": {},
	"mount_driver": {}, "agent_transport": {}, "guest_arch": {}, "gpu": {}, "network": {},
	"proxy_listen": {}, "max_vms": {}, "max_cpus_total": {}, "max_memory_mb_total": {},
	"max_cpus_per_vm": {}, "max_memory_mb_per_vm": {}, "profiles": {}, "check_updates": {},
	"mcp": {}, "warm_pool": {}, "connections": {}, "desktop": {},
}

// Validate checks config for obvious errors after Load/applyDefaults.
func (c Config) Validate() error {
	var errs []string

	switch strings.ToLower(strings.TrimSpace(c.Hypervisor)) {
	case "qemu", "mock", "firecracker":
	default:
		errs = append(errs, fmt.Sprintf("hypervisor: unknown %q (want qemu|mock|firecracker)", c.Hypervisor))
	}

	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Sprintf("log_level: unknown %q (want debug|info|warn|error)", c.LogLevel))
	}

	switch c.MountDriver {
	case "9p", "virtiofs":
	default:
		errs = append(errs, fmt.Sprintf("mount_driver: unknown %q (want 9p|virtiofs)", c.MountDriver))
	}

	switch strings.ToLower(strings.TrimSpace(c.AgentTransport)) {
	case "auto", "tcp", "vsock":
	default:
		errs = append(errs, fmt.Sprintf("agent_transport: unknown %q (want auto|tcp|vsock)", c.AgentTransport))
	}

	if c.GuestArch != "" {
		switch c.GuestArch {
		case "arm64", "amd64":
		default:
			errs = append(errs, fmt.Sprintf("guest_arch: unknown %q (want arm64|amd64 or empty)", c.GuestArch))
		}
	}

	if c.GPU != "" && c.GPU != "virtio" {
		errs = append(errs, fmt.Sprintf("gpu: unknown %q (want empty or virtio)", c.GPU))
	}

	if c.Network != "" {
		switch c.Network {
		case "slirp", "overlay":
		default:
			errs = append(errs, fmt.Sprintf("network: unknown %q (want slirp|overlay)", c.Network))
		}
	}

	if c.DefaultCPUs < 0 || c.DefaultMemoryMB < 0 || c.DefaultDiskGB < 0 {
		errs = append(errs, "cpus/memory_mb/disk_gb must be non-negative")
	}
	if c.MaxVMs < 0 || c.MaxCPUsTotal < 0 || c.MaxMemoryMBTotal < 0 {
		errs = append(errs, "max_vms / max_cpus_total / max_memory_mb_total must be non-negative")
	}
	if c.ReadyTimeout < 0 {
		errs = append(errs, "ready_timeout must be non-negative")
	}

	if c.WarmPool.Size < 0 {
		errs = append(errs, "warm_pool.size must be non-negative")
	}
	if c.WarmPool.Size > 32 {
		errs = append(errs, "warm_pool.size must be <= 32")
	}
	if c.WarmPool.Size > 0 && strings.TrimSpace(c.WarmPool.Template) == "" {
		errs = append(errs, "warm_pool.template is required when warm_pool.size > 0")
	}

	if c.API != "" {
		if _, _, err := net.SplitHostPort(c.API); err != nil {
			errs = append(errs, fmt.Sprintf("api: %v (want host:port)", err))
		}
	}
	if c.APIURL != "" {
		u := NormalizeAPIURL(c.APIURL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			errs = append(errs, fmt.Sprintf("api_url: invalid %q", c.APIURL))
		}
	}

	if c.API != "" {
		host, _, err := net.SplitHostPort(c.API)
		if err == nil && host != "" && host != "127.0.0.1" && host != "localhost" && host != "::1" {
			if c.ResolvedAPIToken() == "" {
				errs = append(errs, "api_token is required when api binds a non-loopback address (including 0.0.0.0)")
			}
		}
	}

	if c.MCP.Listen != "" {
		listen := c.MCP.Listen
		if i := strings.Index(listen, "/"); i > 0 {
			listen = listen[:i]
		}
		if _, _, err := net.SplitHostPort(listen); err != nil {
			if !strings.Contains(c.MCP.Listen, ":") {
				errs = append(errs, fmt.Sprintf("mcp.listen: invalid %q (want host:port)", c.MCP.Listen))
			}
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, "\n"))
}

// ValidateFile loads path (strict unknown keys) and runs Validate.
func ValidateFile(path string) (Config, error) {
	cfg := Defaults()
	if path == "" {
		path = filepathJoinHome("config.yaml")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	// Strict: reject unknown top-level keys so garbage like "urmom:" fails.
	var doc map[string]interface{}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	var unknown []string
	for k := range doc {
		if _, ok := knownTopLevelKeys[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		return cfg, fmt.Errorf("unknown config key(s): %s", strings.Join(unknown, ", "))
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func filepathJoinHome(name string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	return home + "/.grain/" + name
}

var _ = time.Second

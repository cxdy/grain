// Package desktop implements pure Grain Desktop control-plane client logic
// (connection profiles, daemon start policy, lifecycle, shell/logs helpers).
// It is a thin client of the public grain API — not a second control plane.
package desktop

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Connection is a named API target (local unix socket or remote HTTP).
type Connection struct {
	// Name is the stable switcher id (e.g. "local", "lab").
	Name string `yaml:"name" json:"name"`
	// API is the HTTP base URL for remote daemons (e.g. "http://127.0.0.1:7474").
	// Empty means local unix socket.
	API string `yaml:"api,omitempty" json:"api,omitempty"`
	// Token is an optional Bearer token (discouraged in files; prefer TokenEnv).
	Token string `yaml:"token,omitempty" json:"token,omitempty"`
	// TokenEnv is the name of an environment variable holding the Bearer token.
	TokenEnv string `yaml:"token_env,omitempty" json:"token_env,omitempty"`
	// Notes is free text (e.g. SSH tunnel instructions) shown in the UI.
	Notes string `yaml:"notes,omitempty" json:"notes,omitempty"`
	// Socket overrides the local unix socket path when API is empty.
	Socket string `yaml:"socket,omitempty" json:"socket,omitempty"`
	// DataDir is used for local log paths; empty falls back to config DataDir.
	DataDir string `yaml:"data_dir,omitempty" json:"data_dir,omitempty"`
}

// IsLocal reports whether this connection targets a local daemon (unix socket).
// A loopback HTTP API is treated as remote for capability matrix purposes
// (cannot start/stop that engine via `grain up` on this machine unless socket).
func (c Connection) IsLocal() bool {
	api := strings.TrimSpace(c.API)
	if api == "" {
		return true
	}
	return false
}

// ResolvedToken returns the Bearer token from Token, or from TokenEnv / GRAIN_TOKEN.
func (c Connection) ResolvedToken() string {
	if strings.TrimSpace(c.Token) != "" {
		return strings.TrimSpace(c.Token)
	}
	if env := strings.TrimSpace(c.TokenEnv); env != "" {
		return os.Getenv(env)
	}
	return ""
}

// ResolvedSocket returns the unix socket path for a local connection.
func (c Connection) ResolvedSocket(defaultSocket string) string {
	if s := strings.TrimSpace(c.Socket); s != "" {
		return expandHome(s)
	}
	return expandHome(defaultSocket)
}

// LocalConnection builds the implicit local profile.
func LocalConnection(socket, dataDir string) Connection {
	return Connection{
		Name:    "local",
		Socket:  socket,
		DataDir: dataDir,
	}
}

// NormalizeAPIURL trims and ensures a scheme (http:// for bare host:port).
func NormalizeAPIURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.TrimRight(s, "/")
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	return s
}

// IsLoopbackAPI reports whether the API URL host is loopback (127.0.0.1 / ::1 / localhost).
func IsLoopbackAPI(api string) bool {
	api = NormalizeAPIURL(api)
	if api == "" {
		return true
	}
	u, err := url.Parse(api)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// WarnCleartextRemote is true when dialing non-loopback http:// (Bearer sniffable).
func WarnCleartextRemote(api string) bool {
	api = NormalizeAPIURL(api)
	if api == "" {
		return false
	}
	u, err := url.Parse(api)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	return !IsLoopbackAPI(api)
}

// ResolveConnection picks a named connection from the list, or default/local.
func ResolveConnection(conns []Connection, name, defaultName, defaultSocket, dataDir string) (Connection, error) {
	list := EnsureLocalConnection(conns, defaultSocket, dataDir)
	if name == "" {
		name = defaultName
	}
	if name == "" {
		name = "local"
	}
	for _, c := range list {
		if c.Name == name {
			return c, nil
		}
	}
	return Connection{}, fmt.Errorf("unknown connection %q", name)
}

// EnsureLocalConnection guarantees a "local" entry exists at the front if missing.
func EnsureLocalConnection(conns []Connection, defaultSocket, dataDir string) []Connection {
	hasLocal := false
	for _, c := range conns {
		if c.Name == "local" {
			hasLocal = true
			break
		}
	}
	if hasLocal {
		out := make([]Connection, len(conns))
		copy(out, conns)
		for i := range out {
			if out[i].Name == "local" {
				if out[i].Socket == "" {
					out[i].Socket = defaultSocket
				}
				if out[i].DataDir == "" {
					out[i].DataDir = dataDir
				}
			}
			if out[i].API != "" {
				out[i].API = NormalizeAPIURL(out[i].API)
			}
		}
		return out
	}
	local := LocalConnection(defaultSocket, dataDir)
	if len(conns) == 0 {
		return []Connection{local}
	}
	out := make([]Connection, 0, len(conns)+1)
	out = append(out, local)
	for _, c := range conns {
		if c.API != "" {
			c.API = NormalizeAPIURL(c.API)
		}
		out = append(out, c)
	}
	return out
}

// ConnectionNames returns profile names in list order.
func ConnectionNames(conns []Connection) []string {
	names := make([]string, 0, len(conns))
	for _, c := range conns {
		if c.Name != "" {
			names = append(names, c.Name)
		}
	}
	return names
}

func expandHome(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

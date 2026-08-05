package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
)

// apiURLFlag is set from the root persistent flag --api (overrides config/env).
var apiURLFlag string

// effectiveAPIURL returns the remote daemon base URL, or empty for local unix socket.
// Priority: --api flag > env GRAIN_API > config api_url.
func effectiveAPIURL(cfg config.Config) string {
	if v := strings.TrimSpace(apiURLFlag); v != "" {
		return config.NormalizeAPIURL(v)
	}
	if v := strings.TrimSpace(os.Getenv("GRAIN_API")); v != "" {
		return config.NormalizeAPIURL(v)
	}
	return cfg.ResolvedAPIURL()
}

// remoteMode is true when the CLI dials a TCP/HTTP API instead of the unix socket.
func remoteMode(cfg config.Config) bool {
	return effectiveAPIURL(cfg) != ""
}

// clientFrom builds an API client for the local unix socket or a remote HTTP URL.
// Non-loopback remotes require GRAIN_TOKEN / api_token.
//
// When the local unix socket is missing (half-dead daemon after a racy restart)
// but config.api is set, fall back to loopback TCP so CLI ops still work.
func clientFrom(cfg config.Config) (*api.Client, error) {
	if err := requireRemoteAuth(cfg); err != nil {
		return nil, err
	}
	token := os.Getenv("GRAIN_TOKEN")
	if token == "" {
		token = cfg.ResolvedAPIToken()
	}
	base := effectiveAPIURL(cfg)
	if base == "" {
		sock := cfg.Socket
		// Prefer unix when the path is dialable.
		if sockOK(sock) {
			return localUnixClient(sock, token), nil
		}
		// Half-dead control plane: process still serves TCP, socket path gone.
		if local := localLoopbackAPIURL(cfg.API); local != "" {
			return &api.Client{
				Base:  local,
				Token: token,
				HTTP: &http.Client{
					Transport: &http.Transport{
						ResponseHeaderTimeout: 5 * time.Minute,
					},
				},
			}, nil
		}
		return localUnixClient(sock, token), nil
	}
	return &api.Client{
		Base:  base,
		Token: token,
		HTTP: &http.Client{
			// No global Timeout — create waits; use request context instead.
			Transport: &http.Transport{
				ResponseHeaderTimeout: 5 * time.Minute,
			},
		},
	}, nil
}

func localUnixClient(sock, token string) *api.Client {
	return &api.Client{
		Base:  "http://grain",
		Token: token,
		HTTP: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sock)
				},
				ResponseHeaderTimeout: 5 * time.Minute,
			},
		},
	}
}

func sockOK(path string) bool {
	if path == "" {
		return false
	}
	c, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// localLoopbackAPIURL maps a daemon listen addr (e.g. 0.0.0.0:7474) to a CLI
// base URL on loopback. Empty when api is unset or unparseable.
func localLoopbackAPIURL(apiAddr string) string {
	apiAddr = strings.TrimSpace(apiAddr)
	if apiAddr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(apiAddr)
	if err != nil {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// requireRemoteAuth errors when targeting a non-loopback API without a Bearer token.
func requireRemoteAuth(cfg config.Config) error {
	base := effectiveAPIURL(cfg)
	if base == "" {
		return nil
	}
	if config.APIURLIsLoopback(base) {
		return nil
	}
	token := os.Getenv("GRAIN_TOKEN")
	if token == "" {
		token = cfg.ResolvedAPIToken()
	}
	if token == "" {
		return fmt.Errorf("remote API %s requires a Bearer token — set GRAIN_TOKEN or api_token in config (and ensure the daemon has the same api_token)", base)
	}
	return nil
}

// requireLocalDaemon errors when a command must run against a local daemon
// (up/down, host proxy, serial logs, local-only tooling).
func requireLocalDaemon(cfg config.Config, cmd string) error {
	if !remoteMode(cfg) {
		return nil
	}
	return fmt.Errorf("%s requires a local grain daemon (unset GRAIN_API / --api / api_url); for a remote host, SSH in or use the HTTP API for VM ops", cmd)
}

package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
)

// apiURLFlag is set from the root persistent flag --api (overrides config/env).
var apiURLFlag string

// insecureHTTPWarnOnce ensures the cleartext remote-API warning prints at most once per process.
var insecureHTTPWarnOnce sync.Once

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
//
// http:// and https:// bases both work; https uses the default TLS client config.
// A one-time stderr warning is emitted for non-loopback cleartext HTTP (see
// warnInsecureRemoteHTTP / GRAIN_INSECURE_HTTP).
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
	warnInsecureRemoteHTTP(base)
	return &api.Client{
		Base:  base,
		Token: token,
		HTTP: &http.Client{
			// No global Timeout — create waits; use request context instead.
			// Default TLS settings apply for https:// bases (no custom certs).
			Transport: &http.Transport{
				ResponseHeaderTimeout: 5 * time.Minute,
			},
		},
	}, nil
}

// shouldWarnInsecureHTTP reports whether the CLI should warn about cleartext
// remote API transport. base is a normalized or raw API URL; insecureEnv is
// the value of GRAIN_INSECURE_HTTP (1/true/yes/on silences the warning).
func shouldWarnInsecureHTTP(base, insecureEnv string) bool {
	if envTruthy(insecureEnv) {
		return false
	}
	base = config.NormalizeAPIURL(base)
	if base == "" {
		return false
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" {
		return false
	}
	// Only plain HTTP is cleartext; https:// is fine (TLS terminator / reverse proxy).
	if !strings.EqualFold(u.Scheme, "http") {
		return false
	}
	if config.APIURLIsLoopback(base) {
		return false
	}
	return true
}

// warnInsecureRemoteHTTP prints a one-time stderr warning when dialing a
// non-loopback http:// API URL. Bearer tokens travel in cleartext and are
// sniffable on shared networks. Prefer SSH tunnel to 127.0.0.1 or HTTPS.
// Silence with GRAIN_INSECURE_HTTP=1.
func warnInsecureRemoteHTTP(base string) {
	if !shouldWarnInsecureHTTP(base, os.Getenv("GRAIN_INSECURE_HTTP")) {
		return
	}
	insecureHTTPWarnOnce.Do(func() {
		_, _ = fmt.Fprintln(os.Stderr, "warning: remote API uses cleartext HTTP to a non-loopback host — Authorization Bearer tokens can be sniffed; prefer an SSH tunnel to 127.0.0.1 or an HTTPS reverse proxy (set GRAIN_INSECURE_HTTP=1 to silence)")
	})
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

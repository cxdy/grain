// Package mcp implements the grain Model Context Protocol server.
// Tools call the public grain Go client against a running daemon
// (unix socket or HTTP base URL + optional Bearer token).
package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/config"
)

// ConnectOptions selects how the MCP server reaches the grain daemon.
// Precedence matches the CLI: GRAIN_API / APIURL, then unix socket.
type ConnectOptions struct {
	// APIURL is an HTTP base URL (e.g. http://127.0.0.1:7474).
	// Env GRAIN_API when empty.
	APIURL string
	// Token is sent as Authorization: Bearer when non-empty.
	// Env GRAIN_TOKEN when empty.
	Token string
	// Socket is a local unix socket path. Used when APIURL is empty.
	// Env GRAIN_SOCKET, else ~/.grain/grain.sock (or config data_dir).
	Socket string
	// ConfigPath optional grain config file for data_dir/socket defaults.
	ConfigPath string
}

// ConnectFromEnv builds ConnectOptions from the process environment and optional config file.
func ConnectFromEnv() ConnectOptions {
	return ConnectOptions{
		APIURL:     strings.TrimSpace(os.Getenv("GRAIN_API")),
		Token:      strings.TrimSpace(os.Getenv("GRAIN_TOKEN")),
		Socket:     strings.TrimSpace(os.Getenv("GRAIN_SOCKET")),
		ConfigPath: strings.TrimSpace(os.Getenv("GRAIN_CONFIG")),
	}
}

// Dial opens a grain client using opts (env-compatible defaults applied).
func Dial(opts ConnectOptions) (*client.Client, error) {
	api := strings.TrimSpace(opts.APIURL)
	if api == "" {
		api = strings.TrimSpace(os.Getenv("GRAIN_API"))
	}
	token := strings.TrimSpace(opts.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GRAIN_TOKEN"))
	}
	api = config.NormalizeAPIURL(api)

	if api != "" {
		c, err := client.DialHTTP(api, token)
		if err != nil {
			return nil, fmt.Errorf("dial grain API %s: %w", api, err)
		}
		c.SetUserAgent("grain-mcp")
		return c, nil
	}

	sock := strings.TrimSpace(opts.Socket)
	if sock == "" {
		sock = strings.TrimSpace(os.Getenv("GRAIN_SOCKET"))
	}
	if sock == "" {
		cfg, err := config.Load(opts.ConfigPath)
		if err != nil {
			// Still try default path.
			home, _ := os.UserHomeDir()
			sock = filepath.Join(home, ".grain", "grain.sock")
		} else {
			sock = cfg.Socket
			if token == "" {
				token = cfg.ResolvedAPIToken()
			}
		}
	}
	if sock == "" {
		return nil, fmt.Errorf("no grain daemon target: set GRAIN_API or GRAIN_SOCKET (or start local grain up)")
	}
	c, err := client.DialUnixToken(sock, token)
	if err != nil {
		return nil, fmt.Errorf("dial grain socket %s: %w", sock, err)
	}
	c.SetUserAgent("grain-mcp")
	return c, nil
}

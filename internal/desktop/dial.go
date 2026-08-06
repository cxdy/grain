package desktop

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/cxdy/grain/client"
)

// DialFunc opens a grain API client for a connection (injectable).
type DialFunc func(conn Connection, cfg Config) (*client.Client, error)

// EffectiveToken returns GRAIN_TOKEN, then connection token, then config api_token.
func EffectiveToken(conn Connection, cfg Config) string {
	if t := strings.TrimSpace(os.Getenv("GRAIN_TOKEN")); t != "" {
		return t
	}
	if t := conn.ResolvedToken(); t != "" {
		return t
	}
	return cfg.ResolvedAPIToken()
}

// SocketOK reports whether a unix socket path accepts a quick dial (CLI parity).
func SocketOK(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	c, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// LocalLoopbackAPIURL maps a daemon listen addr (e.g. 0.0.0.0:7474) to a
// client base URL on loopback — same behavior as the grain CLI.
func LocalLoopbackAPIURL(apiAddr string) string {
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

// DialTarget is the resolved transport for a connection.
type DialTarget struct {
	UseUnix bool
	Socket  string
	BaseURL string // HTTP base when !UseUnix
	Token   string
}

// ResolveDialTarget picks unix socket or loopback/remote HTTP (CLI parity).
func ResolveDialTarget(conn Connection, cfg Config) (DialTarget, error) {
	token := EffectiveToken(conn, cfg)
	if !conn.IsLocal() {
		api := NormalizeAPIURL(conn.API)
		if api == "" {
			return DialTarget{}, fmt.Errorf("connection %q: api URL is empty", conn.Name)
		}
		return DialTarget{BaseURL: api, Token: token}, nil
	}
	sock := conn.ResolvedSocket(cfg.Socket)
	if SocketOK(sock) {
		return DialTarget{UseUnix: true, Socket: sock, Token: token}, nil
	}
	// Half-dead control plane: TCP still up, socket path gone (common with
	// api: 0.0.0.0:7474 deploys that never bind a unix socket).
	if loop := LocalLoopbackAPIURL(cfg.API); loop != "" {
		return DialTarget{BaseURL: loop, Token: token}, nil
	}
	if sock == "" {
		return DialTarget{}, fmt.Errorf("local connection %q: no dialable socket and no config api", conn.Name)
	}
	// Last resort: return unix target so Dial surfaces a clear connect error.
	return DialTarget{UseUnix: true, Socket: sock, Token: token}, nil
}

// DialConnection opens a client for conn using the public grain client package.
// Local: prefer unix socket; if undialable, fall back to loopback TCP from config.api.
func DialConnection(conn Connection, cfg Config) (*client.Client, error) {
	t, err := ResolveDialTarget(conn, cfg)
	if err != nil {
		return nil, err
	}
	if t.UseUnix {
		return client.DialUnixToken(t.Socket, t.Token)
	}
	return client.DialHTTP(t.BaseURL, t.Token)
}


package desktop

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// ShellSessionInfo describes how to open a PTY shell for a VM.
type ShellSessionInfo struct {
	// URL is the WebSocket HTTP URL (http/https; dialer upgrades).
	URL string `json:"url"`
	// Token is the Bearer token to send (may be empty).
	Token string `json:"token"`
	// UseUnix is true when dialing via unix socket (Local).
	UseUnix bool `json:"use_unix"`
	// Socket is the unix socket path when UseUnix.
	Socket string `json:"socket,omitempty"`
	// VM is the sandbox name.
	VM string `json:"vm"`
	// Cols / Rows initial size.
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// BuildShellSession constructs dial info for GET /vms/{name}/shell.
// Prefer BuildShellSessionCfg so local TCP fallback matches the API dial path.
func BuildShellSession(conn Connection, defaultSocket, defaultToken, vm string, cols, rows int) (ShellSessionInfo, error) {
	cfg := Config{Socket: defaultSocket, APIToken: defaultToken}
	return BuildShellSessionCfg(conn, cfg, vm, cols, rows)
}

// BuildShellSessionCfg builds shell dial info using the same transport as API dial.
func BuildShellSessionCfg(conn Connection, cfg Config, vm string, cols, rows int) (ShellSessionInfo, error) {
	if strings.TrimSpace(vm) == "" {
		return ShellSessionInfo{}, fmt.Errorf("vm name is required")
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	q := url.Values{}
	q.Set("cols", strconv.Itoa(cols))
	q.Set("rows", strconv.Itoa(rows))

	target, err := ResolveDialTarget(conn, cfg)
	if err != nil {
		return ShellSessionInfo{}, err
	}
	info := ShellSessionInfo{
		Token: target.Token,
		VM:    vm,
		Cols:  cols,
		Rows:  rows,
	}
	path := "/vms/" + url.PathEscape(vm) + "/shell?" + q.Encode()
	if target.UseUnix {
		info.UseUnix = true
		info.Socket = target.Socket
		info.URL = "http://grain" + path
		return info, nil
	}
	info.URL = strings.TrimRight(target.BaseURL, "/") + path
	return info, nil
}

// ShellDialer opens a websocket to the shell endpoint (injectable transport).
type ShellDialer func(ctx context.Context, info ShellSessionInfo) (*websocket.Conn, error)

// DefaultShellDial dials the daemon shell WebSocket (unix or TCP).
func DefaultShellDial(ctx context.Context, info ShellSessionInfo) (*websocket.Conn, error) {
	hdr := http.Header{}
	if info.Token != "" {
		hdr.Set("Authorization", "Bearer "+info.Token)
	}
	opts := &websocket.DialOptions{
		HTTPHeader: hdr,
	}
	if info.UseUnix {
		if info.Socket == "" {
			return nil, fmt.Errorf("unix shell dial: empty socket")
		}
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", info.Socket)
				},
				ResponseHeaderTimeout: 2 * time.Minute,
			},
		}
	}
	conn, _, err := websocket.Dial(ctx, info.URL, opts)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// ShellIO copies between a websocket shell and local readers/writers until ctx cancel.
func ShellIO(ctx context.Context, conn *websocket.Conn, stdin io.Reader, stdout io.Writer) error {
	if conn == nil {
		return fmt.Errorf("nil websocket")
	}
	errCh := make(chan error, 2)

	go func() {
		buf := make([]byte, 32*1024)
		for {
			if stdin == nil {
				errCh <- nil
				return
			}
			n, rerr := stdin.Read(buf)
			if n > 0 {
				werr := conn.Write(ctx, websocket.MessageBinary, buf[:n])
				if werr != nil {
					errCh <- werr
					return
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					errCh <- nil
					return
				}
				errCh <- rerr
				return
			}
		}
	}()

	go func() {
		for {
			_, data, rerr := conn.Read(ctx)
			if rerr != nil {
				errCh <- rerr
				return
			}
			if stdout != nil && len(data) > 0 {
				if _, werr := stdout.Write(data); werr != nil {
					errCh <- werr
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close(websocket.StatusNormalClosure, "ctx done")
		return ctx.Err()
	case err := <-errCh:
		_ = conn.Close(websocket.StatusNormalClosure, "")
		if err == nil || err == io.EOF {
			return nil
		}
		if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
			return nil
		}
		return err
	}
}

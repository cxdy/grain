package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/mdlayher/vsock"
)

// DefaultVsockPort is the guest vsock port grain-agent listens on
// (same numeric port as the TCP listen).
const DefaultVsockPort = 7475

// FirecrackerVsockSocket is the host UDS basename under a Firecracker VM
// directory. Must match hypervisor.FCVsockName.
const FirecrackerVsockSocket = "fc-vsock.sock"

// Target identifies how the host reaches a guest grain-agent.
type Target struct {
	// CID is the guest virtio-vsock context ID. 0 means not using host AF_VSOCK.
	CID int
	// Port is the host-side TCP port forwarded to guest :7475.
	Port int
	// FirecrackerUDS is the host unix socket for Firecracker's vsock device
	// (CONNECT <port>\n protocol). When set, Dial prefers this over AF_VSOCK.
	// See https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md
	FirecrackerUDS string
}

// HasEndpoint reports whether the target has any agent transport configured.
func (t Target) HasEndpoint() bool {
	return t.FirecrackerUDS != "" || t.CID > 0 || t.Port > 0
}

// TargetForInstance builds a Dial target from persisted instance agent fields.
//
// Firecracker guests have no TCP hostfwd (AgentPort=0) and a guest CID set for
// the FC vsock device. Host reachability is UDS + CONNECT at
// dirname(diskPath)/fc-vsock.sock — not host AF_VSOCK (QEMU vhost-vsock only).
func TargetForInstance(agentCID, agentPort int, diskPath string) Target {
	t := Target{CID: agentCID, Port: agentPort}
	if agentPort <= 0 && agentCID > 0 && diskPath != "" {
		t.FirecrackerUDS = filepath.Join(filepath.Dir(diskPath), FirecrackerVsockSocket)
		// Host AF_VSOCK does not speak Firecracker's CONNECT protocol.
		t.CID = 0
	}
	return t
}

// Dial connects to the guest agent for t.
//
// Order:
//  1. Firecracker host UDS + CONNECT (when FirecrackerUDS is set)
//  2. Host AF_VSOCK when CID > 0 (QEMU vhost-vsock)
//  3. TCP hostfwd at http://127.0.0.1:Port
//
// The returned Client is ready for HTTP calls; callers should still check
// health when needed. ctx bounds the connectivity probe.
func Dial(ctx context.Context, t Target) (*Client, error) {
	if !t.HasEndpoint() {
		return nil, fmt.Errorf("agent dial: no vsock CID, Firecracker UDS, or TCP port")
	}

	if t.FirecrackerUDS != "" {
		if c, err := dialFirecrackerUDS(ctx, t.FirecrackerUDS, DefaultVsockPort); err == nil {
			return c, nil
		} else if t.Port <= 0 && t.CID <= 0 {
			return nil, fmt.Errorf("agent dial: firecracker vsock %s: %w", t.FirecrackerUDS, err)
		}
		// Fall through to AF_VSOCK / TCP if configured.
	}

	if t.CID > 0 {
		if c, err := dialVsock(ctx, uint32(t.CID), DefaultVsockPort); err == nil {
			return c, nil
		}
		// Fall through to TCP hostfwd when vsock is unreachable.
	}

	if t.Port <= 0 {
		return nil, fmt.Errorf("agent dial: no reachable transport (cid=%d uds=%q)", t.CID, t.FirecrackerUDS)
	}
	return &Client{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", t.Port),
	}, nil
}

// firecrackerUDSDial opens and handshakes a Firecracker host vsock UDS (tests override).
var firecrackerUDSDial = func(udsPath string, guestPort uint32) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", udsPath, 3*time.Second)
	if err != nil {
		return nil, err
	}
	if err := fcVsockCONNECT(conn, guestPort); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// fcVsockCONNECT performs the Firecracker host-side vsock CONNECT handshake.
// Reads the response line one byte at a time so no buffered data is lost for
// the subsequent HTTP stream on the same connection.
func fcVsockCONNECT(conn net.Conn, guestPort uint32) error {
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", guestPort); err != nil {
		return fmt.Errorf("CONNECT write: %w", err)
	}
	var buf []byte
	tmp := make([]byte, 1)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			if tmp[0] == '\n' {
				break
			}
			buf = append(buf, tmp[0])
			if len(buf) > 128 {
				return fmt.Errorf("CONNECT response too long")
			}
		}
		if err != nil {
			return fmt.Errorf("CONNECT read: %w", err)
		}
	}
	line := strings.TrimSpace(string(buf))
	if strings.HasPrefix(line, "OK") {
		_ = conn.SetDeadline(time.Time{})
		return nil
	}
	return fmt.Errorf("CONNECT rejected: %s", line)
}

// dialFirecrackerUDS builds a Client that dials Firecracker host vsock UDS for each request.
func dialFirecrackerUDS(ctx context.Context, udsPath string, guestPort uint32) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type dialResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	go func() {
		c, err := firecrackerUDSDial(udsPath, guestPort)
		ch <- dialResult{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		_ = r.conn.Close()
	}

	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			type res struct {
				c   net.Conn
				err error
			}
			done := make(chan res, 1)
			go func() {
				c, err := firecrackerUDSDial(udsPath, guestPort)
				done <- res{c, err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-done:
				return r.c, r.err
			}
		},
	}
	return &Client{
		BaseURL: "http://fc-vsock",
		HTTP: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		},
	}, nil
}

// vsockDial opens an AF_VSOCK connection (overridable in tests).
var vsockDial = func(cid, port uint32) (net.Conn, error) {
	return vsock.Dial(cid, port, nil)
}

// dialVsock builds a Client that dials AF_VSOCK for every HTTP connection.
// It probes connectivity once so Dial can fall back to TCP when vsock is down.
func dialVsock(ctx context.Context, cid, port uint32) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	type dialResult struct {
		conn net.Conn
		err  error
	}
	ch := make(chan dialResult, 1)
	go func() {
		c, err := vsockDial(cid, port)
		ch <- dialResult{c, err}
	}()

	var conn net.Conn
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		conn = r.conn
	}
	_ = conn.Close()

	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			type res struct {
				c   net.Conn
				err error
			}
			done := make(chan res, 1)
			go func() {
				c, err := vsockDial(cid, port)
				done <- res{c, err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-done:
				return r.c, r.err
			}
		},
	}
	return &Client{
		// Host is ignored; DialContext always opens vsock cid:port.
		BaseURL: "http://vsock",
		HTTP: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		},
	}, nil
}

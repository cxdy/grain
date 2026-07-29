package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/mdlayher/vsock"
)

// DefaultVsockPort is the guest vsock port grain-agent listens on
// (same numeric port as the TCP listen).
const DefaultVsockPort = 7475

// Target identifies how the host reaches a guest grain-agent.
type Target struct {
	// CID is the guest virtio-vsock context ID. 0 means TCP only.
	CID int
	// Port is the host-side TCP port forwarded to guest :7475.
	Port int
}

// HasEndpoint reports whether the target has any agent transport configured.
func (t Target) HasEndpoint() bool {
	return t.CID > 0 || t.Port > 0
}

// Dial connects to the guest agent for t.
//
// When CID > 0, prefers virtio-vsock (CID:DefaultVsockPort). If the vsock
// dial fails (or the platform has no vsock), falls back to TCP hostfwd at
// http://127.0.0.1:Port. When CID is 0, uses TCP only.
//
// The returned Client is ready for HTTP calls; callers should still check
// health when needed. ctx bounds the optional vsock connectivity probe.
func Dial(ctx context.Context, t Target) (*Client, error) {
	if !t.HasEndpoint() {
		return nil, fmt.Errorf("agent dial: no vsock CID or TCP port")
	}

	if t.CID > 0 {
		if c, err := dialVsock(ctx, uint32(t.CID), DefaultVsockPort); err == nil {
			return c, nil
		}
		// Fall through to TCP hostfwd when vsock is unreachable.
	}

	if t.Port <= 0 {
		return nil, fmt.Errorf("agent dial: vsock cid=%d unreachable and no TCP port", t.CID)
	}
	return &Client{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", t.Port),
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

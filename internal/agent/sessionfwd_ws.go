package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	fwdOpen  = "fwd_open"
	fwdData  = "fwd_data"
	fwdClose = "fwd_close"
	fwdAgent = "agent"
	fwdProxy = "proxy"
)

type wsFwdHub struct {
	conn *websocket.Conn
	wmu  sync.Mutex
	mu   sync.Mutex
	half map[string]*fwdNetConn
}

func newWsFwdHub(conn *websocket.Conn) *wsFwdHub {
	return &wsFwdHub{conn: conn, half: make(map[string]*fwdNetConn)}
}

func (h *wsFwdHub) writeCtrl(ctx context.Context, ctrl ShellControl) error {
	payload, err := json.Marshal(ctrl)
	if err != nil {
		return err
	}
	h.wmu.Lock()
	defer h.wmu.Unlock()
	return h.conn.Write(ctx, websocket.MessageText, payload)
}

// Handle consumes a control frame. Returns true if it was a fwd_* type.
func (h *wsFwdHub) Handle(ctrl ShellControl) bool {
	switch ctrl.Type {
	case fwdData:
		h.mu.Lock()
		c := h.half[ctrl.Id]
		h.mu.Unlock()
		if c == nil {
			return true
		}
		b, err := base64.StdEncoding.DecodeString(ctrl.Data)
		if err != nil || len(b) == 0 {
			return true
		}
		select {
		case c.rch <- b:
		case <-c.done:
		}
		return true
	case fwdClose:
		h.mu.Lock()
		c := h.half[ctrl.Id]
		h.mu.Unlock()
		if c != nil {
			c.closeLocal()
		}
		return true
	default:
		return false
	}
}

func (h *wsFwdHub) attach(id string) *fwdNetConn {
	c := &fwdNetConn{
		hub:  h,
		id:   id,
		rch:  make(chan []byte, 16),
		done: make(chan struct{}),
	}
	h.mu.Lock()
	h.half[id] = c
	h.mu.Unlock()
	return c
}

func (h *wsFwdHub) drop(id string) {
	h.mu.Lock()
	delete(h.half, id)
	h.mu.Unlock()
}

// ServeGuestConn announces a new guest-side connection and relays bytes.
func (h *wsFwdHub) ServeGuestConn(ctx context.Context, kind string, raw net.Conn) {
	id := randomClipboardID()
	fc := h.attach(id)
	defer func() {
		_ = fc.Close()
		_ = raw.Close()
	}()
	if err := h.writeCtrl(ctx, ShellControl{Type: fwdOpen, Id: id, Chan: kind}); err != nil {
		return
	}
	_ = relayConns(ctx, fc, raw)
}

// AcceptOpen is called on the client when the agent sends fwd_open.
func (h *wsFwdHub) AcceptOpen(id, kind string) *fwdNetConn {
	_ = kind
	return h.attach(id)
}

type fwdNetConn struct {
	hub      *wsFwdHub
	id       string
	rch      chan []byte
	leftover []byte
	done     chan struct{}
	closed   atomic.Bool
}

func (c *fwdNetConn) Read(p []byte) (int, error) {
	if len(c.leftover) > 0 {
		n := copy(p, c.leftover)
		c.leftover = c.leftover[n:]
		return n, nil
	}
	select {
	case <-c.done:
		return 0, io.EOF
	case b, ok := <-c.rch:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, b)
		if n < len(b) {
			c.leftover = b[n:]
		}
		return n, nil
	}
}

func (c *fwdNetConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := c.hub.writeCtrl(ctx, ShellControl{
		Type: fwdData,
		Id:   c.id,
		Data: base64.StdEncoding.EncodeToString(p),
	})
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *fwdNetConn) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.done)
	c.hub.drop(c.id)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.hub.writeCtrl(ctx, ShellControl{Type: fwdClose, Id: c.id})
	return nil
}

func (c *fwdNetConn) closeLocal() {
	if c.closed.CompareAndSwap(false, true) {
		close(c.done)
		c.hub.drop(c.id)
	}
}

func (c *fwdNetConn) LocalAddr() net.Addr              { return fwdAddr(c.id) }
func (c *fwdNetConn) RemoteAddr() net.Addr             { return fwdAddr(c.id) }
func (c *fwdNetConn) SetDeadline(time.Time) error      { return nil }
func (c *fwdNetConn) SetReadDeadline(time.Time) error  { return nil }
func (c *fwdNetConn) SetWriteDeadline(time.Time) error { return nil }

type fwdAddr string

func (a fwdAddr) Network() string { return "fwd" }
func (a fwdAddr) String() string  { return string(a) }

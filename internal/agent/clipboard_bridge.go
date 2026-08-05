package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// clipboardBridge links guest GET /clipboard (pbpaste) to the interactive shell
// client's host clipboard over the shell WebSocket control channel.
type clipboardBridge struct {
	mu      sync.Mutex
	sendGet func(id string) error // send clipboard_get to client; nil if no shell
	waiters map[string]chan clipboardResult
}

type clipboardResult struct {
	data []byte
	err  string
}

func newClipboardBridge() *clipboardBridge {
	return &clipboardBridge{waiters: make(map[string]chan clipboardResult)}
}

// setSender registers the active shell session's ability to request clipboard.
// Pass nil when the shell ends.
func (b *clipboardBridge) setSender(fn func(id string) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sendGet = fn
}

// deliver resolves a pending request (client → agent clipboard frame).
func (b *clipboardBridge) deliver(id string, data []byte, errMsg string) {
	b.mu.Lock()
	ch := b.waiters[id]
	if ch != nil {
		delete(b.waiters, id)
	}
	b.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- clipboardResult{data: data, err: errMsg}:
	default:
	}
}

// request asks the shell client for clipboard contents.
func (b *clipboardBridge) request(ctx context.Context) ([]byte, error) {
	id := randomClipboardID()
	ch := make(chan clipboardResult, 1)

	b.mu.Lock()
	send := b.sendGet
	if send == nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("no interactive grain sh session for clipboard paste")
	}
	b.waiters[id] = ch
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.waiters, id)
		b.mu.Unlock()
	}()

	if err := send(id); err != nil {
		return nil, err
	}

	timeout := 5 * time.Second
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("clipboard paste timed out waiting for grain sh client")
	case res := <-ch:
		if res.err != "" {
			return nil, fmt.Errorf("%s", res.err)
		}
		return res.data, nil
	}
}

func randomClipboardID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// handleClipboard serves GET /clipboard for guest pbpaste shims.
func (s *Server) handleClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.clip == nil {
		http.Error(w, "clipboard bridge unavailable", http.StatusServiceUnavailable)
		return
	}
	data, err := s.clip.request(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

// registerShellClipboard wires a shell WebSocket so pbpaste can reach the client.
func (s *Server) registerShellClipboard(conn *websocket.Conn) (unregister func()) {
	if s.clip == nil {
		return func() {}
	}
	s.clip.setSender(func(id string) error {
		payload, _ := json.Marshal(ShellControl{Type: "clipboard_get", Id: id})
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return conn.Write(ctx, websocket.MessageText, payload)
	})
	return func() {
		s.clip.setSender(nil)
	}
}

// handleShellClipboardControl processes client→agent clipboard replies.
func (s *Server) handleShellClipboardControl(ctrl ShellControl) {
	if s.clip == nil || ctrl.Type != "clipboard" || ctrl.Id == "" {
		return
	}
	if ctrl.Error != "" {
		s.clip.deliver(ctrl.Id, nil, ctrl.Error)
		return
	}
	data, err := decodeClipboardData(ctrl.Data)
	if err != nil {
		s.clip.deliver(ctrl.Id, nil, err.Error())
		return
	}
	s.clip.deliver(ctrl.Id, data, "")
}

func decodeClipboardData(b64 string) ([]byte, error) {
	if b64 == "" {
		return []byte{}, nil
	}
	return base64.StdEncoding.DecodeString(b64)
}

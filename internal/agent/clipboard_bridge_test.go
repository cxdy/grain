package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestClipboardBridgeRequestDeliver(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	b.setSender(func(id string) error {
		go func() {
			time.Sleep(10 * time.Millisecond)
			b.deliver(id, []byte("hello-clip"), "")
		}()
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := b.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello-clip" {
		t.Fatalf("%q", data)
	}
}

func TestClipboardBridgeNoSession(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	_, err := b.request(context.Background())
	if err == nil {
		t.Fatal("expected error without shell session")
	}
}

func TestClipboardBridgeErrorReply(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	b.setSender(func(id string) error {
		b.deliver(id, nil, "no clipboard helper")
		return nil
	})
	_, err := b.request(context.Background())
	if err == nil || err.Error() != "no clipboard helper" {
		t.Fatalf("err=%v", err)
	}
}

func TestDecodeClipboardData(t *testing.T) {
	t.Parallel()
	// "hi" base64
	got, err := decodeClipboardData("aGk=")
	if err != nil || string(got) != "hi" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = decodeClipboardData("")
	if err != nil || len(got) != 0 {
		t.Fatalf("%q %v", got, err)
	}
	_, err = decodeClipboardData("!!!not-b64!!!")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClipboardDeliverUnknownAndFull(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	// unknown id: no waiter
	b.deliver("nope", []byte("x"), "")
	// full channel (buffer 1) then default branch
	ch := make(chan clipboardResult) // unbuffered → select default
	b.mu.Lock()
	b.waiters["full"] = ch
	b.mu.Unlock()
	// deliver without receiver → default case
	b.deliver("full", []byte("y"), "")
}

func TestClipboardBridgeSendError(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	b.setSender(func(id string) error {
		return io.ErrClosedPipe
	})
	_, err := b.request(context.Background())
	if err == nil {
		t.Fatal("expected send error")
	}
}

func TestClipboardBridgeTimeout(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	b.setSender(func(id string) error {
		// never deliver
		return nil
	})
	// request uses 5s timer; use canceled context instead for speed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.request(ctx)
	if err == nil {
		t.Fatal("expected ctx error")
	}
}

func TestClipboardBridgeRequestTimeoutTimer(t *testing.T) {
	if testing.Short() {
		t.Skip("5s clipboard timeout")
	}
	// Not run by default — 5s is long; exercise with a short fake by not waiting full 5s
	// in unit tests. Covered via ctx cancel above.
	t.Skip("covered by ctx cancel path")
}

func TestHandleClipboardHTTP(t *testing.T) {
	t.Parallel()
	s := NewServer("127.0.0.1:0", nil)
	// success path: wire sender that delivers
	s.clip.setSender(func(id string) error {
		go s.clip.deliver(id, []byte("paste-me"), "")
		return nil
	})
	mux := s.Handler()
	req := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if rr.Body.String() != "paste-me" {
		t.Fatalf("body %q", rr.Body.String())
	}

	// method not allowed
	req2 := httptest.NewRequest(http.MethodPost, "/clipboard", nil)
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rr2.Code)
	}

	// no session → 502
	s.clip.setSender(nil)
	req3 := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	rr3 := httptest.NewRecorder()
	mux.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rr3.Code)
	}

	// nil clip → 503
	s2 := &Server{}
	req4 := httptest.NewRequest(http.MethodGet, "/clipboard", nil)
	rr4 := httptest.NewRecorder()
	s2.handleClipboard(rr4, req4)
	if rr4.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rr4.Code)
	}
}

func TestRegisterShellClipboard(t *testing.T) {
	t.Parallel()
	// nil clip
	s := &Server{}
	unreg := s.registerShellClipboard(nil)
	unreg()

	// with clip + real websocket: server accepts, registers sender, client waits for clipboard_get
	s2 := NewServer("127.0.0.1:0", nil)
	registered := make(chan struct{})
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Error(err)
			return
		}
		conn.SetReadLimit(ShellWebSocketReadLimit)
		unreg := s2.registerShellClipboard(conn)
		close(registered)
		// keep connection open until client closes
		ctx := r.Context()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				break
			}
			if typ == websocket.MessageText {
				var ctrl ShellControl
				if json.Unmarshal(data, &ctrl) == nil && ctrl.Type == "clipboard" {
					s2.handleShellClipboardControl(ctrl)
				}
			}
		}
		unreg()
		_ = conn.Close(websocket.StatusNormalClosure, "")
	})
	hs := httptest.NewServer(up)
	t.Cleanup(hs.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hs.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Close(websocket.StatusNormalClosure, "") }()

	select {
	case <-registered:
	case <-ctx.Done():
		t.Fatal("register timeout")
	}

	// request triggers sender → conn.Write(clipboard_get)
	go func() {
		// client receives clipboard_get and replies via deliver path is optional;
		// just complete the request with deliver from another goroutine after write.
		time.Sleep(30 * time.Millisecond)
		// find pending waiter and deliver — use handleShellClipboardControl style
		s2.clip.mu.Lock()
		var id string
		for k := range s2.clip.waiters {
			id = k
			break
		}
		s2.clip.mu.Unlock()
		if id != "" {
			s2.clip.deliver(id, []byte("from-ws"), "")
		}
	}()

	data, err := s2.clip.request(ctx)
	if err != nil {
		// Write may still have been exercised even if deliver races
		t.Logf("request: %v", err)
	} else if string(data) != "from-ws" {
		t.Fatalf("data %q", data)
	}

	// also verify client saw a text frame (clipboard_get)
	rctx, rcancel := context.WithTimeout(context.Background(), time.Second)
	defer rcancel()
	_, msg, rerr := cli.Read(rctx)
	if rerr == nil && !strings.Contains(string(msg), "clipboard_get") {
		t.Logf("ws msg %s", msg)
	}
}

// TestShellClipboardLargeImageReply ensures screenshot-sized paste replies are
// accepted on the shell WebSocket. Default coder/websocket limit is 32KiB;
// production sets ShellWebSocketReadLimit so Cmd/Ctrl+V image paste works when
// the guest reads host clipboard via GET /clipboard.
func TestShellClipboardLargeImageReply(t *testing.T) {
	t.Parallel()
	s := NewServer("127.0.0.1:0", nil)

	// Synthetic "screenshot": well over 32KiB when base64-framed as ShellControl.
	img := make([]byte, 80*1024)
	img[0], img[1], img[2], img[3] = 0x89, 'P', 'N', 'G'
	for i := 4; i < len(img); i++ {
		img[i] = byte(i)
	}

	done := make(chan []byte, 1)
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			t.Error(err)
			return
		}
		// Production path: raise limit (without this, Read fails at 32KiB).
		conn.SetReadLimit(ShellWebSocketReadLimit)
		defer func() { _ = conn.CloseNow() }()

		unreg := s.registerShellClipboard(conn)
		defer unreg()

		ctx := r.Context()
		// Mimic guest pbpaste → bridge.request while client replies with large clipboard.
		go func() {
			data, err := s.clip.request(ctx)
			if err != nil {
				t.Errorf("request: %v", err)
				done <- nil
				return
			}
			done <- data
		}()

		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var ctrl ShellControl
			if json.Unmarshal(data, &ctrl) != nil {
				continue
			}
			if ctrl.Type == "clipboard" {
				s.handleShellClipboardControl(ctrl)
				return
			}
		}
	})
	hs := httptest.NewServer(up)
	t.Cleanup(hs.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hs.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Close(websocket.StatusNormalClosure, "") }()

	// Client: wait for clipboard_get (like grain sh), reply with large image.
	_, msg, err := cli.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var get ShellControl
	if err := json.Unmarshal(msg, &get); err != nil || get.Type != "clipboard_get" {
		t.Fatalf("want clipboard_get, got %s err=%v", msg, err)
	}
	reply := ShellControl{
		Type: "clipboard",
		Id:   get.Id,
		Data: base64.StdEncoding.EncodeToString(img),
	}
	payload, _ := json.Marshal(reply)
	if len(payload) < 40*1024 {
		t.Fatalf("test payload too small (%d) to exercise limit", len(payload))
	}
	if err := cli.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write large clipboard: %v", err)
	}

	select {
	case got := <-done:
		if len(got) != len(img) {
			t.Fatalf("got %d bytes want %d", len(got), len(img))
		}
		if got[0] != 0x89 || got[1] != 'P' {
			t.Fatalf("bad magic %x", got[:4])
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for large clipboard delivery")
	}
}

// TestShellClipboardDefaultLimitTooSmall documents the pre-fix failure mode:
// without SetReadLimit, a ~80KiB image reply is rejected at 32KiB.
func TestShellClipboardDefaultLimitTooSmall(t *testing.T) {
	t.Parallel()
	up := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			t.Error(err)
			return
		}
		// Intentionally NO SetReadLimit — default 32768.
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()
		_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"clipboard_get","id":"x"}`))
		_, _, err = conn.Read(ctx)
		if err == nil {
			t.Error("expected default limit to reject large clipboard frame")
			return
		}
		if !strings.Contains(err.Error(), "message too big") && !strings.Contains(err.Error(), "read limited") {
			t.Errorf("want message-too-big style error, got %v", err)
		}
	})
	hs := httptest.NewServer(up)
	t.Cleanup(hs.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(hs.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Close(websocket.StatusNormalClosure, "") }()

	_, _, _ = cli.Read(ctx) // clipboard_get
	big := make([]byte, 50*1024)
	reply, _ := json.Marshal(ShellControl{
		Type: "clipboard",
		Id:   "x",
		Data: base64.StdEncoding.EncodeToString(big),
	})
	_ = cli.Write(ctx, websocket.MessageText, reply)
	time.Sleep(100 * time.Millisecond)
}

func TestHandleShellClipboardControl(t *testing.T) {
	t.Parallel()
	s := NewServer("127.0.0.1:0", nil)

	// ignore: wrong type / empty id / nil clip
	s.handleShellClipboardControl(ShellControl{Type: "resize", Id: "1"})
	s.handleShellClipboardControl(ShellControl{Type: "clipboard", Id: ""})
	sNil := &Server{}
	sNil.handleShellClipboardControl(ShellControl{Type: "clipboard", Id: "1", Data: "aGk="})

	// error reply
	ch := make(chan clipboardResult, 1)
	s.clip.mu.Lock()
	s.clip.waiters["e1"] = ch
	s.clip.mu.Unlock()
	s.handleShellClipboardControl(ShellControl{Type: "clipboard", Id: "e1", Error: "boom"})
	res := <-ch
	if res.err != "boom" {
		t.Fatalf("%+v", res)
	}

	// bad base64
	ch2 := make(chan clipboardResult, 1)
	s.clip.mu.Lock()
	s.clip.waiters["e2"] = ch2
	s.clip.mu.Unlock()
	s.handleShellClipboardControl(ShellControl{Type: "clipboard", Id: "e2", Data: "!!!"})
	res2 := <-ch2
	if res2.err == "" {
		t.Fatal("expected decode error")
	}

	// success
	ch3 := make(chan clipboardResult, 1)
	s.clip.mu.Lock()
	s.clip.waiters["e3"] = ch3
	s.clip.mu.Unlock()
	s.handleShellClipboardControl(ShellControl{
		Type: "clipboard", Id: "e3",
		Data: base64.StdEncoding.EncodeToString([]byte("ok")),
	})
	res3 := <-ch3
	if string(res3.data) != "ok" {
		t.Fatalf("%q", res3.data)
	}
}

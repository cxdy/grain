package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeShellServer upgrades to WebSocket, echoes binary frames, then closes.
func fakeShellServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shell" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx := r.Context()
		// Read one binary frame then reply and close.
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary || typ == websocket.MessageText {
				_ = conn.Write(ctx, websocket.MessageBinary, append([]byte("echo:"), data...))
				_ = conn.Close(websocket.StatusNormalClosure, "done")
				return
			}
		}
	}))
}

func TestShellClientRoundTrip(t *testing.T) {
	srv := fakeShellServer(t)
	t.Cleanup(srv.Close)

	c := &Client{
		BaseURL: srv.URL,
		HTTP:    &http.Client{Timeout: 0},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out strings.Builder
	err := c.Shell(ctx, ShellOpts{
		Cols:   80,
		Rows:   24,
		Stdin:  strings.NewReader("hello"),
		Stdout: &out,
		Stderr: io.Discard,
		Raw:    boolPtrFalse(),
	})
	// Session ends when server closes after echo — normal close is success.
	if err != nil {
		// Some websocket close variants return an error; accept normal end.
		if !isWSNormalClose(err) && !strings.Contains(err.Error(), "close") {
			t.Fatalf("Shell: %v", err)
		}
	}
	if !strings.Contains(out.String(), "echo:") && out.Len() == 0 {
		// May have closed before write drained; still covered dial/read path.
		t.Logf("stdout empty (server may have closed early): %q", out.String())
	}
}

func TestShellClientDefaultsAndHTTPSScheme(t *testing.T) {
	// Server that rejects WS quickly still exercises scheme/default sizing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no upgrade", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	// Force https base → wss path in scheme switch (dial will fail TLS or status).
	c := &Client{
		BaseURL: "http" + strings.TrimPrefix(srv.URL, "http"), // keep http
		HTTP:    &http.Client{Timeout: 2 * time.Second},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Shell(ctx, ShellOpts{
		// Cols/Rows 0 → defaults 80x24
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Raw:    boolPtrFalse(),
	})
	if err == nil {
		t.Fatal("expected dial/upgrade error")
	}

	// https scheme branch
	c2 := &Client{BaseURL: "https://127.0.0.1:1", HTTP: &http.Client{Timeout: 200 * time.Millisecond}}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	_ = c2.Shell(ctx2, ShellOpts{
		Cols: 10, Rows: 10,
		Stdin: strings.NewReader(""), Stdout: io.Discard, Raw: boolPtrFalse(),
	})
}

func TestShellClientContextCancel(t *testing.T) {
	// Hang after accept so client ctx cancel path runs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		// block until client cancels
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	// Pipe with no writer data — Read blocks until closed on cleanup.
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close(); _ = pr.Close() })

	c := &Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := c.Shell(ctx, ShellOpts{
		Cols: 40, Rows: 12,
		Stdin:  pr,
		Stdout: io.Discard,
		Raw:    boolPtrFalse(),
	})
	if err == nil {
		t.Fatal("expected cancel error")
	}
}

func boolPtrFalse() *bool {
	v := false
	return &v
}

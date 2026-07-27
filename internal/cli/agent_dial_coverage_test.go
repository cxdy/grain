package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/api"
)

// startFakeAgent serves agent /health HEAD and /exec stream+buffered on a TCP port.
func startFakeAgent(t *testing.T, streamHandler func(w http.ResponseWriter, r *http.Request)) (port int, closeFn func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hostname": "guest", "agent_version": "test",
			})
		}
	})
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		if streamHandler != nil {
			streamHandler(w, r)
			return
		}
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"type":"started","pid":1}` + "\n"))
			_, _ = w.Write([]byte(`{"type":"stdout","data":"hello\n"}` + "\n"))
			_, _ = w.Write([]byte(`{"type":"stderr","data":"warn\n"}` + "\n"))
			_, _ = w.Write([]byte(`{"type":"exit","exit_code":0}` + "\n"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": "buf\n", "stderr": "", "exit_code": 0,
		})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(portStr)
	return p, func() { _ = srv.Close(); _ = ln.Close() }
}

func TestDialGuestAgentSuccess(t *testing.T) {
	port, closeFn := startFakeAgent(t, nil)
	defer closeFn()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm1", "status": "running", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	ac, err := dialGuestAgent(c, "vm1", true)
	if err != nil {
		t.Fatal(err)
	}
	if ac == nil || ac.BaseURL == "" {
		t.Fatal("nil client")
	}
	if !strings.Contains(ac.BaseURL, strconv.Itoa(port)) {
		t.Fatalf("base %s", ac.BaseURL)
	}
}

func TestDialGuestAgentHealthFail(t *testing.T) {
	// Listen but no /health → HeadHealth fails
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Accept and close immediately (or hang) — better: serve 503
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 503)
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close(); _ = ln.Close() }()
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "status": "running", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	if _, err := dialGuestAgent(c, "vm", false); err != errAgentSkip {
		t.Fatalf("skip: %v", err)
	}
	if _, err := dialGuestAgent(c, "vm", true); err == nil || !strings.Contains(err.Error(), "agent not available") {
		t.Fatalf("force health: %v", err)
	}
}

func TestExecViaAgentStreamOK(t *testing.T) {
	port, closeFn := startFakeAgent(t, nil)
	defer closeFn()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "status": "running", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	if err := execViaAgent(c, "vm", []string{"echo", "hi"}, true, false); err != nil {
		t.Fatal(err)
	}
}

func TestExecViaAgentNonZero(t *testing.T) {
	port, closeFn := startFakeAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"type":"exit","exit_code":9}` + "\n"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"exit_code": 9})
	})
	defer closeFn()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	err := execViaAgent(c, "vm", []string{"false"}, true, false)
	if err == nil {
		t.Fatal("expected exit error")
	}
	if ec, ok := err.(exitCodeError); !ok || int(ec) != 9 {
		t.Fatalf("got %v", err)
	}
}

func TestExecViaAgentBufferedFallback(t *testing.T) {
	port, closeFn := startFakeAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			// Fail before started so buffered fallback runs
			http.Error(w, "stream down", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": "from-buf\n", "stderr": "e\n", "exit_code": 0,
		})
	})
	defer closeFn()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	if err := execViaAgent(c, "vm", []string{"echo"}, false, false); err != nil {
		t.Fatal(err)
	}
}

func TestExecViaAgentStreamStartedThenFail(t *testing.T) {
	port, closeFn := startFakeAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			// Flush started then close without exit → scanner ends, client may error
			_, _ = w.Write([]byte(`{"type":"started","pid":7}` + "\n"))
			// force connection reset by hijack close mid-stream is hard;
			// send error frame instead after started
			_, _ = w.Write([]byte(`{"type":"error","error":"boom"}` + "\n"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"exit_code": 0})
	})
	defer closeFn()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	// force=true and started=true → should return stream error without re-run
	err := execViaAgent(c, "vm", []string{"x"}, true, false)
	if err == nil {
		t.Fatal("expected error after started")
	}
}

func TestExecViaAgentBufferedNonZeroWithErrorField(t *testing.T) {
	port, closeFn := startFakeAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			http.Error(w, "no", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": "", "stderr": "", "exit_code": 2, "error": "failed",
		})
	})
	defer closeFn()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	err := execViaAgent(c, "vm", []string{"x"}, true, false)
	if err == nil {
		t.Fatal("expected exit 2")
	}
}

func TestExecViaAgentDialSkip(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", 404)
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}
	if err := execViaAgent(c, "x", []string{"true"}, false, false); err != errAgentSkip {
		t.Fatalf("got %v", err)
	}
}

func TestExecViaDaemonAPIBothFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 500)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	err := execViaDaemonAPI(c, "vm", []string{"true"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecViaDaemonAPIBufferedNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			http.Error(w, "no stream", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": "x", "stderr": "y", "exit_code": 4,
		})
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	err := execViaDaemonAPI(c, "vm", []string{"false"})
	if err == nil {
		t.Fatal("expected exit")
	}
	if ec, ok := err.(exitCodeError); !ok || int(ec) != 4 {
		t.Fatalf("%v", err)
	}
}

func TestDaemonHTTPWithTokenAndTransport(t *testing.T) {
	var saw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		saw = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	base := &http.Client{Transport: http.DefaultTransport}
	c := &api.Client{Base: srv.URL, Token: "abc", HTTP: base}
	hc := daemonHTTP(c)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	res, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if saw != "Bearer abc" {
		t.Fatalf("auth %q", saw)
	}
}

func TestShellViaDaemonURLConstruction(t *testing.T) {
	// shellViaDaemon will try websocket and fail — still covers path setup
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no ws", 400)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client(), Token: "t"}
	err := shellViaDaemon(c, "my-vm")
	if err == nil {
		t.Fatal("expected shell error")
	}
	// Ensure name is path-escaped in BaseURL construction (no panic)
	err = shellViaDaemon(c, "vm/with spaces")
	if err == nil {
		t.Fatal("expected error")
	}
	_ = url.PathEscape("x")
	_ = fmt.Sprintf("%v", err)
}

func TestExecViaAgentBufferedForceError(t *testing.T) {
	port, closeFn := startFakeAgent(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			http.Error(w, "no stream", 500)
			return
		}
		// malformed so decode fails... actually returns JSON with error path via force
		http.Error(w, "buf fail", 500)
	})
	defer closeFn()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "agent_port": port, "agent_cid": 0,
		})
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}

	err := execViaAgent(c, "vm", []string{"x"}, true, false)
	if err == nil {
		t.Fatal("expected agent exec error")
	}
}

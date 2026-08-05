package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/client"
	grainmcp "github.com/cxdy/grain/internal/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHTTPEndpoint(t *testing.T) {
	t.Parallel()
	if grainmcp.HTTPEndpoint("127.0.0.1:7476") != "http://127.0.0.1:7476/mcp" {
		t.Fatal(grainmcp.HTTPEndpoint("127.0.0.1:7476"))
	}
	if grainmcp.HTTPEndpoint("") != "http://127.0.0.1:7476/mcp" {
		t.Fatal(grainmcp.HTTPEndpoint(""))
	}
}

func TestRunHTTPStreamableListTools(t *testing.T) {
	// Mock grain daemon for the MCP tools client.
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "grain", "version": "t"})
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*client.Instance{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)

	gc, err := client.DialHTTP(daemon.URL, "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Pick free port via :0 by using httptest reverse: run MCP on a listener we control.
	// RunHTTP binds listen string — use a temporary approach with Streamable handler unit.
	srv := grainmcp.NewMCPServer("t", gc)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{JSONResponse: true})
	mcpHTTP := httptest.NewServer(handler)
	t.Cleanup(mcpHTTP.Close)

	cli := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v0"}, nil)
	sess, err := cli.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mcpHTTP.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) < 8 {
		t.Fatalf("tools %d", len(tools.Tools))
	}
	// Call health through streamable HTTP → real client → mock daemon.
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolHealth})
	if err != nil {
		t.Fatal(err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "ok") {
		t.Fatalf("%s", text)
	}

}

func mockDaemonClient(t *testing.T) *client.Client {
	t.Helper()
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "grain", "version": "t"})
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*client.Instance{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(daemon.Close)
	c, err := client.DialHTTP(daemon.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func freeLoopback(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitHTTP(t *testing.T, url string, header http.Header) *http.Response {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		for k, vs := range header {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			return res
		}
		last = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server not ready: %v", last)
	return nil
}

func TestRunHTTPAuthRequiresBearer(t *testing.T) {
	t.Parallel()
	c := mockDaemonClient(t)
	addr := freeLoopback(t)
	const token = "mcp-secret"

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- grainmcp.RunHTTP(ctx, addr, "t", c, "", slog.New(slog.NewTextHandler(io.Discard, nil)), token)
	}()

	// unauthorized without token
	res := waitHTTP(t, "http://"+addr+"/", nil)
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d %s", res.StatusCode, body)
	}

	// wrong token
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer wrong")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 wrong token, got %d", res.StatusCode)
	}

	// authorized with Bearer
	req, err = http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("want 200 with token, got %d %s", res.StatusCode, body)
	}
	if !strings.Contains(string(body), "/mcp") {
		t.Fatalf("body %q", body)
	}

	// /mcp also protected
	req, err = http.NewRequest(http.MethodGet, "http://"+addr+grainmcp.DefaultHTTPPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/mcp without token: %d", res.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunHTTP did not exit")
	}
}

func TestRunHTTPNonLoopbackEmptyToken(t *testing.T) {
	t.Parallel()
	c := mockDaemonClient(t)
	// High unused port; must fail before bind when token empty.
	err := grainmcp.RunHTTP(context.Background(), "0.0.0.0:59999", "t", c, "", slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	if err == nil {
		t.Fatal("expected non-loopback without token error")
	}
	if !strings.Contains(err.Error(), "not loopback") && !strings.Contains(err.Error(), "api_token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunHTTPLoopbackEmptyTokenOK(t *testing.T) {
	t.Parallel()
	c := mockDaemonClient(t)
	addr := freeLoopback(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errCh := make(chan error, 1)
	go func() {
		errCh <- grainmcp.RunHTTP(ctx, addr, "t", c, "", slog.New(slog.NewTextHandler(io.Discard, nil)), "")
	}()

	res := waitHTTP(t, "http://"+addr+"/", nil)
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("loopback without token: %d %s", res.StatusCode, body)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunHTTP did not exit")
	}
}

package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cxdy/grain/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultHTTPPath is the Streamable HTTP mount path for the MCP server.
const DefaultHTTPPath = "/mcp"

// DefaultListen is the default host:port when MCP HTTP is enabled.
const DefaultListen = "127.0.0.1:7476"

// HTTPEndpoint returns the full URL clients should use (listen host:port + path).
func HTTPEndpoint(listen string) string {
	if listen == "" {
		listen = DefaultListen
	}
	return "http://" + listen + DefaultHTTPPath
}

// RunHTTP serves Streamable HTTP MCP until ctx is cancelled.
// c is the grain daemon client (typically unix socket or loopback API).
func RunHTTP(ctx context.Context, listen, version string, c *client.Client, log *slog.Logger) error {
	if listen == "" {
		listen = DefaultListen
	}
	if log == nil {
		log = slog.Default()
	}
	srv := NewMCPServer(version, c)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return srv
	}, &mcp.StreamableHTTPOptions{
		// Stateless + JSON responses: concurrent tool calls from hosts (e.g. Grok)
		// each get a full request/response without sharing a sticky session stream.
		Stateless:    true,
		JSONResponse: true,
	})
	mux := http.NewServeMux()
	mux.Handle(DefaultHTTPPath, handler)
	mux.Handle(DefaultHTTPPath+"/", handler)
	// Convenience root redirect message for humans hitting the port in a browser.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "grain MCP Streamable HTTP — use %s\n", DefaultHTTPPath)
	})

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("mcp listen %s: %w", listen, err)
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("mcp listen", "addr", listen, "path", DefaultHTTPPath, "url", HTTPEndpoint(listen))
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shCtx)
		return nil
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	}
}

// RunStdio serves MCP over stdin/stdout (IDE hosts: Claude Code, Codex, …).
func RunStdio(ctx context.Context, version string, c *client.Client) error {
	srv := NewMCPServer(version, c)
	return srv.Run(ctx, &mcp.StdioTransport{})
}

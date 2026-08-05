package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
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
// dataDir is used for image list/pull and serial/qemu logs (may be empty).
// token, when non-empty, requires Authorization: Bearer <token> on all routes
// (same policy as the daemon TCP API). When token is empty, listen must be
// loopback-only; non-loopback binds without a token are refused.
func RunHTTP(ctx context.Context, listen, version string, c *client.Client, dataDir string, log *slog.Logger, token string) error {
	if listen == "" {
		listen = DefaultListen
	}
	if log == nil {
		log = slog.Default()
	}

	// Mirror daemon API bind policy: refuse non-loopback without a token.
	if !config.ListenAddrIsLoopback(listen) && token == "" {
		return fmt.Errorf("mcp listen %q is not loopback but api_token is empty — set api_token (or bind 127.0.0.1) before exposing MCP; see https://grainvm.com/guides/remote-host/", listen)
	}
	if !config.ListenAddrIsLoopback(listen) {
		log.Warn("mcp listen is not loopback — ensure host firewall and api_token; prefer 127.0.0.1 + SSH tunnel or TLS reverse proxy",
			"addr", listen)
	}

	srv := NewMCPServerOpts(ServerOptions{Version: version, Client: c, DataDir: dataDir})
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
		Handler:           bearerAuthMiddleware(token, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return fmt.Errorf("mcp listen %s: %w", listen, err)
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("mcp listen", "addr", listen, "path", DefaultHTTPPath, "url", HTTPEndpoint(listen), "auth", token != "")
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

// bearerAuthMiddleware requires Authorization: Bearer <token> when token is non-empty.
func bearerAuthMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !api.BearerAuthorized(r.Header.Get("Authorization"), token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RunStdio serves MCP over stdin/stdout (IDE hosts: Claude Code, Codex, …).
func RunStdio(ctx context.Context, version string, c *client.Client, dataDir string) error {
	srv := NewMCPServerOpts(ServerOptions{Version: version, Client: c, DataDir: dataDir})
	return srv.Run(ctx, &mcp.StdioTransport{})
}

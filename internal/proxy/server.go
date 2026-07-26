package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cxdy/grain/internal/secrets"
)

// SecretReader loads secret bytes by name (typically *secrets.Store).
type SecretReader interface {
	ReadData(name string) ([]byte, error)
}

// Server is a default-deny HTTP(S) forward proxy with allow rules and optional secret injection.
type Server struct {
	Store   *Store
	Secrets SecretReader
	Listen  string
	Log     *slog.Logger

	// DialContext for outbound connections (tests can override).
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)

	mu       sync.Mutex
	httpSrv  *http.Server
	listener net.Listener
}

// NewServer builds a Server with defaults.
func NewServer(store *Store, secrets SecretReader, listen string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if listen == "" {
		listen = DefaultListen
	}
	s := &Server{
		Store:   store,
		Secrets: secrets,
		Listen:  listen,
		Log:     log,
	}
	s.DialContext = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	return s
}

// Handler returns the proxy HTTP handler (for tests / custom listeners).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

// ListenAndServe binds and serves until Shutdown or error.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.httpSrv = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}
	s.mu.Unlock()
	s.Log.Info("proxy listen", "addr", s.Listen)
	return s.httpSrv.Serve(ln)
}

// Addr returns the bound address (after ListenAndServe started), or configured Listen.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.Listen
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}

	if r.Method == http.MethodConnect {
		s.handleConnect(w, r)
		return
	}
	s.handleHTTP(w, r)
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	required, err := s.Store.AuthRequired()
	if err != nil {
		s.Log.Error("proxy auth check", "err", err)
		http.Error(w, "proxy auth error", http.StatusInternalServerError)
		return false
	}
	if !required {
		return true
	}
	tok := extractProxyToken(r)
	ok, err := s.Store.ValidToken(tok)
	if err != nil {
		http.Error(w, "proxy auth error", http.StatusInternalServerError)
		return false
	}
	if !ok {
		w.Header().Set("Proxy-Authenticate", `Basic realm="grain-proxy"`)
		http.Error(w, "proxy authentication required", http.StatusProxyAuthRequired)
		return false
	}
	return true
}

// extractProxyToken reads token from Proxy-Authorization (Basic or Bearer) or URL userinfo.
func extractProxyToken(r *http.Request) string {
	if r.URL != nil && r.URL.User != nil {
		// http://token@host:port — username is token; password ignored
		if u := r.URL.User.Username(); u != "" {
			return u
		}
	}
	h := r.Header.Get("Proxy-Authorization")
	if h == "" {
		return ""
	}
	const basic = "Basic "
	const bearer = "Bearer "
	switch {
	case strings.HasPrefix(h, basic):
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(h[len(basic):]))
		if err != nil {
			return ""
		}
		// user:pass or just user
		parts := strings.SplitN(string(raw), ":", 2)
		return parts[0]
	case strings.HasPrefix(strings.ToLower(h), strings.ToLower(bearer)):
		return strings.TrimSpace(h[len(bearer):])
	default:
		return strings.TrimSpace(h)
	}
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	hostport := r.Host
	if hostport == "" {
		hostport = r.URL.Host
	}
	host := StripHostPort(hostport)
	// Ensure port for dial (default 443 for CONNECT).
	if _, _, err := net.SplitHostPort(hostport); err != nil {
		hostport = net.JoinHostPort(host, "443")
	}

	rules, err := s.Store.ListRules()
	if err != nil {
		http.Error(w, "rules error", http.StatusInternalServerError)
		return
	}
	rule := FirstMatch(rules, http.MethodConnect, host, "")
	if rule == nil {
		s.Log.Info("proxy deny", "method", "CONNECT", "host", host)
		http.Error(w, "forbidden: no allow rule matches", http.StatusForbidden)
		return
	}

	// Hijack and tunnel. Secret injection does not apply to opaque TLS tunnels.
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	dest, err := s.DialContext(ctx, "tcp", hostport)
	if err != nil {
		s.Log.Info("proxy connect dial failed", "host", hostport, "err", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	clientConn, bufrw, err := hj.Hijack()
	if err != nil {
		_ = dest.Close()
		return
	}

	// 200 Connection Established
	_, _ = bufrw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	_ = bufrw.Flush()

	s.Log.Info("proxy allow", "method", "CONNECT", "host", host, "rule", rule.ID)
	bidirCopy(clientConn, dest)
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Absolute-form URI required for forward proxy; fall back to Host header.
	targetURL := r.URL
	host := targetURL.Host
	if host == "" {
		host = r.Host
	}
	hostname := StripHostPort(host)
	path := targetURL.Path
	if path == "" {
		path = "/"
	}
	if targetURL.RawQuery != "" {
		// path_prefix match uses path only (no query)
		_ = targetURL.RawQuery
	}

	rules, err := s.Store.ListRules()
	if err != nil {
		http.Error(w, "rules error", http.StatusInternalServerError)
		return
	}
	rule := FirstMatch(rules, r.Method, hostname, path)
	if rule == nil {
		s.Log.Info("proxy deny", "method", r.Method, "host", hostname, "path", path)
		http.Error(w, "forbidden: no allow rule matches", http.StatusForbidden)
		return
	}

	// Build outbound request.
	outURL := *targetURL
	if !outURL.IsAbs() {
		// Reconstruct absolute URL from Host.
		scheme := "http"
		outURL.Scheme = scheme
		outURL.Host = host
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Copy headers except hop-by-hop and proxy auth.
	copyHeaders(outReq.Header, r.Header)
	removeHopByHop(outReq.Header)
	outReq.Header.Del("Proxy-Authorization")
	outReq.Header.Del("Proxy-Connection")

	// Optional secret injection.
	if rule.SecretName != "" && s.Secrets != nil {
		data, err := s.Secrets.ReadData(rule.SecretName)
		if err != nil {
			s.Log.Info("proxy secret missing", "secret", rule.SecretName, "err", err)
			http.Error(w, "secret not found for allow rule", http.StatusBadGateway)
			return
		}
		injectAuthorization(outReq.Header, data)
	}

	client := &http.Client{
		// Do not follow redirects as the proxy — return them to the client.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:           s.DialContext,
			ResponseHeaderTimeout: 60 * time.Second,
			// Disable HTTP/2 to keep streaming simple.
			ForceAttemptHTTP2: false,
		},
		// No Timeout — long downloads OK; rely on context.
	}

	resp, err := client.Do(outReq)
	if err != nil {
		s.Log.Info("proxy upstream error", "host", hostname, "err", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	s.Log.Info("proxy allow", "method", r.Method, "host", hostname, "path", path, "rule", rule.ID, "status", resp.StatusCode)

	removeHopByHop(resp.Header)
	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// injectAuthorization sets Authorization from secret bytes.
// If data already looks like "Bearer …" or "Basic …", use as full header value;
// otherwise prefix with "Bearer ".
func injectAuthorization(h http.Header, data []byte) {
	v := strings.TrimSpace(string(data))
	if v == "" {
		return
	}
	lower := strings.ToLower(v)
	if strings.HasPrefix(lower, "bearer ") || strings.HasPrefix(lower, "basic ") {
		h.Set("Authorization", v)
		return
	}
	// Raw "Authorization: value" form
	if strings.HasPrefix(lower, "authorization:") {
		h.Set("Authorization", strings.TrimSpace(v[len("authorization:"):]))
		return
	}
	h.Set("Authorization", "Bearer "+v)
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

var hopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func removeHopByHop(h http.Header) {
	// Connection token headers
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				h.Del(f)
			}
		}
	}
	for _, k := range hopHeaders {
		h.Del(k)
	}
}

func bidirCopy(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		closeWrite(a)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		closeWrite(b)
	}()
	wg.Wait()
}

func closeWrite(c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
}

// Ensure secrets.Store implements SecretReader at compile time.
var _ SecretReader = (*secrets.Store)(nil)

// GuestProxyEnv returns cloud-init / shell env assignments for guest HTTPS_PROXY.
// listenHostPort is host-side bind (e.g. 0.0.0.0:3128); guest always uses 10.0.2.2:port.
func GuestProxyEnv(token string, listen string) (httpProxy, httpsProxy, noProxy string) {
	port := DefaultGuestProxyPort
	if listen != "" {
		if _, p, err := net.SplitHostPort(listen); err == nil {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				port = n
			}
		}
	}
	var userinfo string
	if token != "" {
		userinfo = token + "@"
	}
	u := fmt.Sprintf("http://%s%s:%d", userinfo, DefaultGuestProxyHost, port)
	return u, u, "localhost,127.0.0.1,10.0.2.0/24"
}

// GuestProxyCloudConfig returns a #cloud-config snippet that exports proxy env.
func GuestProxyCloudConfig(token, listen string) string {
	httpP, httpsP, noP := GuestProxyEnv(token, listen)
	return fmt.Sprintf(`#cloud-config
write_files:
  - path: /etc/profile.d/grain-proxy.sh
    permissions: '0644'
    content: |
      export HTTP_PROXY=%q
      export HTTPS_PROXY=%q
      export http_proxy=%q
      export https_proxy=%q
      export NO_PROXY=%q
      export no_proxy=%q
  - path: /etc/environment
    permissions: '0644'
    content: |
      HTTP_PROXY=%q
      HTTPS_PROXY=%q
      NO_PROXY=%q
`, httpP, httpsP, httpP, httpsP, noP, noP, httpP, httpsP, noP)
}

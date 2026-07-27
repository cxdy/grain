package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestInjectAuthorizationVariants(t *testing.T) {
	t.Parallel()
	h := make(http.Header)
	injectAuthorization(h, []byte("   "))
	if h.Get("Authorization") != "" {
		t.Fatal("empty should no-op")
	}
	h = make(http.Header)
	injectAuthorization(h, []byte("Authorization: Bearer from-prefix"))
	if h.Get("Authorization") != "Bearer from-prefix" {
		t.Fatalf("got %q", h.Get("Authorization"))
	}
	h = make(http.Header)
	injectAuthorization(h, []byte("authorization: Basic abc"))
	if h.Get("Authorization") != "Basic abc" {
		t.Fatalf("got %q", h.Get("Authorization"))
	}
}

func TestExtractProxyToken(t *testing.T) {
	t.Parallel()
	// URL userinfo
	req := httptest.NewRequest(http.MethodGet, "http://mytoken@proxy/path", nil)
	if got := extractProxyToken(req); got != "mytoken" {
		t.Fatalf("userinfo %q", got)
	}
	// Bearer
	req = httptest.NewRequest(http.MethodGet, "http://proxy/", nil)
	req.Header.Set("Proxy-Authorization", "Bearer tok-bearer")
	if got := extractProxyToken(req); got != "tok-bearer" {
		t.Fatalf("bearer %q", got)
	}
	// Basic user:pass
	req = httptest.NewRequest(http.MethodGet, "http://proxy/", nil)
	raw := base64.StdEncoding.EncodeToString([]byte("useronly:secret"))
	req.Header.Set("Proxy-Authorization", "Basic "+raw)
	if got := extractProxyToken(req); got != "useronly" {
		t.Fatalf("basic %q", got)
	}
	// Basic just user
	req = httptest.NewRequest(http.MethodGet, "http://proxy/", nil)
	raw = base64.StdEncoding.EncodeToString([]byte("solo"))
	req.Header.Set("Proxy-Authorization", "Basic "+raw)
	if got := extractProxyToken(req); got != "solo" {
		t.Fatalf("basic solo %q", got)
	}
	// invalid basic
	req = httptest.NewRequest(http.MethodGet, "http://proxy/", nil)
	req.Header.Set("Proxy-Authorization", "Basic !!!")
	if got := extractProxyToken(req); got != "" {
		t.Fatalf("invalid basic %q", got)
	}
	// raw fallback
	req = httptest.NewRequest(http.MethodGet, "http://proxy/", nil)
	req.Header.Set("Proxy-Authorization", "raw-token-value")
	if got := extractProxyToken(req); got != "raw-token-value" {
		t.Fatalf("raw %q", got)
	}
	// empty
	req = httptest.NewRequest(http.MethodGet, "http://proxy/", nil)
	if got := extractProxyToken(req); got != "" {
		t.Fatalf("empty %q", got)
	}
}

func TestGuestProxyCloudConfigAndListen(t *testing.T) {
	t.Parallel()
	cfg := GuestProxyCloudConfig("tok", "127.0.0.1:9999")
	if !strings.Contains(cfg, "#cloud-config") {
		t.Fatal(cfg)
	}
	if !strings.Contains(cfg, "tok@10.0.2.2:9999") {
		t.Fatalf("token/port: %s", cfg)
	}
	if !strings.Contains(cfg, "/etc/profile.d/grain-proxy.sh") {
		t.Fatal(cfg)
	}
	// empty listen → default port
	httpP, _, _ := GuestProxyEnv("", "")
	if !strings.Contains(httpP, "10.0.2.2:3128") {
		t.Fatalf("%s", httpP)
	}
	if !strings.HasPrefix(httpP, "http://10.0.2.2") {
		t.Fatalf("no token: %s", httpP)
	}
	if ListenFromConfig("") != DefaultListen {
		t.Fatal(ListenFromConfig(""))
	}
	if ListenFromConfig("1.2.3.4:5") != "1.2.3.4:5" {
		t.Fatal(ListenFromConfig("1.2.3.4:5"))
	}
	if !strings.HasSuffix(PIDPath("/data"), "proxy/proxy.pid") {
		t.Fatal(PIDPath("/data"))
	}
}

func TestNewServerDefaultsAndShutdown(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(st, nil, "", nil)
	if srv.Listen != DefaultListen {
		t.Fatalf("listen %q", srv.Listen)
	}
	if srv.Log == nil {
		t.Fatal("log")
	}
	// Shutdown before listen is no-op
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.Addr() != DefaultListen {
		t.Fatalf("addr %q", srv.Addr())
	}

	// Real listen + shutdown
	srv2 := NewServer(st, nil, "127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv2.ListenAndServe() }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a := srv2.Addr()
		if a != "" && a != "127.0.0.1:0" && !strings.HasSuffix(a, ":0") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if strings.HasSuffix(srv2.Addr(), ":0") {
		t.Fatalf("did not bind: %s", srv2.Addr())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv2.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit")
	}
}

func TestRemoveHopByHop(t *testing.T) {
	t.Parallel()
	h := make(http.Header)
	h.Set("Connection", "X-Custom, Keep-Alive")
	h.Set("X-Custom", "1")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Authorization", "Bearer x")
	removeHopByHop(h)
	if h.Get("X-Custom") != "" || h.Get("Keep-Alive") != "" {
		t.Fatalf("hop headers remain: %v", h)
	}
	if h.Get("Authorization") != "Bearer x" {
		t.Fatal("auth should stay")
	}
}

func TestStoreReplaceRuleAndClientHelpers(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r1, err := st.AddRule(Rule{Host: "a.com"})
	if err != nil {
		t.Fatal(err)
	}
	// replace same id
	r2, err := st.AddRule(Rule{ID: r1.ID, Host: "b.com", Method: "get"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Host != "b.com" || r2.Method != "GET" {
		t.Fatalf("%+v", r2)
	}
	list, _ := st.ListRules()
	if len(list) != 1 {
		t.Fatalf("len %d", len(list))
	}
	// host required
	if _, err := st.AddRule(Rule{}); err == nil {
		t.Fatal("host required")
	}
	// remove missing
	if err := st.RemoveRule("nope"); err == nil {
		t.Fatal("expected not found")
	}
	// empty name client
	c, err := st.CreateClient("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name == "" || c.ID == "" {
		t.Fatalf("%+v", c)
	}
	tok, err := st.FirstClientToken()
	if err != nil || tok == "" {
		t.Fatalf("token %q %v", tok, err)
	}
	// second client for nextClientName
	c2, err := st.CreateClient("")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Name == c.Name {
		t.Fatalf("names should differ: %q %q", c.Name, c2.Name)
	}
}

func TestFirstClientTokenEmpty(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := st.FirstClientToken()
	if err != nil || tok != "" {
		t.Fatalf("%q %v", tok, err)
	}
}

func TestMatchHostIPv6Brackets(t *testing.T) {
	t.Parallel()
	if !MatchHost("::1", "[::1]") {
		t.Fatal("ipv6 brackets")
	}
	if StripHostPort("[::1]:443") != "::1" {
		t.Fatal(StripHostPort("[::1]:443"))
	}
	if StripHostPort("") != "" {
		t.Fatal("empty")
	}
}

func TestRuleMatchEmptyHost(t *testing.T) {
	t.Parallel()
	r := Rule{}
	if r.Match("GET", "x.com", "/") {
		t.Fatal("empty host rule")
	}
	r = Rule{Host: "x.com", PathPrefix: "/v1"}
	if r.Match("GET", "x.com", "") {
		t.Fatal("empty path should not match /v1 prefix")
	}
	if !r.Match("GET", "x.com", "/v1/x") {
		t.Fatal("should match")
	}
}

// secretReaderFunc adapts a function to SecretReader.
type secretReaderFunc func(name string) ([]byte, error)

func (f secretReaderFunc) ReadData(name string) ([]byte, error) { return f(name) }

func TestProxySecretReaderError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(upstream.Close)

	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewRequest(http.MethodGet, upstream.URL, nil).URL.Hostname()
	_, _ = st.AddRule(Rule{Host: host, SecretName: "x"})

	srv := NewServer(st, secretReaderFunc(func(name string) ([]byte, error) {
		return nil, fmt.Errorf("secret gone")
	}), "127.0.0.1:0", nil)
	proxyHTTP := httptest.NewServer(srv.Handler())
	t.Cleanup(proxyHTTP.Close)

	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	res, err := doViaProxy(proxyHTTP.URL, "", req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502 got %d", res.StatusCode)
	}
}

func TestProxyHTTPUpstreamDialFail(t *testing.T) {
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.AddRule(Rule{Host: "127.0.0.1"})
	srv := NewServer(st, nil, "127.0.0.1:0", nil)
	srv.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, fmt.Errorf("dial blocked")
	}
	proxyHTTP := httptest.NewServer(srv.Handler())
	t.Cleanup(proxyHTTP.Close)

	// Absolute-form GET to a closed port on loopback
	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:1/", nil)
	res, err := doViaProxy(proxyHTTP.URL, "", req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("want 502 got %d", res.StatusCode)
	}
}

func TestProxyBearerAuthHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(upstream.Close)

	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewRequest(http.MethodGet, upstream.URL, nil).URL.Hostname()
	_, _ = st.AddRule(Rule{Host: host})
	c, err := st.CreateClient("b")
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(st, nil, "127.0.0.1:0", nil)
	proxyHTTP := httptest.NewServer(srv.Handler())
	t.Cleanup(proxyHTTP.Close)

	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	req.Header.Set("Proxy-Authorization", "Bearer "+c.Token)
	proxyU, err := url.Parse(proxyHTTP.URL)
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{
		Proxy: http.ProxyURL(proxyU),
	}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

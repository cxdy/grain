package proxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/secrets"
)

func TestInjectAuthorization(t *testing.T) {
	t.Parallel()
	h := make(http.Header)
	injectAuthorization(h, []byte("sk-abc"))
	if h.Get("Authorization") != "Bearer sk-abc" {
		t.Fatalf("got %q", h.Get("Authorization"))
	}
	h = make(http.Header)
	injectAuthorization(h, []byte("Bearer already"))
	if h.Get("Authorization") != "Bearer already" {
		t.Fatalf("got %q", h.Get("Authorization"))
	}
	h = make(http.Header)
	injectAuthorization(h, []byte("Basic xyz"))
	if h.Get("Authorization") != "Basic xyz" {
		t.Fatalf("got %q", h.Get("Authorization"))
	}
}

func TestGuestProxyEnv(t *testing.T) {
	t.Parallel()
	httpP, httpsP, noP := GuestProxyEnv("tok123", "0.0.0.0:3128")
	if !strings.Contains(httpP, "tok123@10.0.2.2:3128") {
		t.Fatalf("http %s", httpP)
	}
	if httpsP != httpP {
		t.Fatal(httpsP)
	}
	if !strings.Contains(noP, "127.0.0.1") {
		t.Fatal(noP)
	}
}

func TestHTTPProxyAllowDenyAndSecret(t *testing.T) {
	// Upstream echo server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Auth", r.Header.Get("Authorization"))
		w.Header().Set("X-Path", r.URL.Path)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)

	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sec, err := secrets.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sec.Put(secrets.PutRequest{
		Name:       "api-key",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("secret-token")),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Allow only host of upstream with secret, path /v1/
	_, err = st.AddRule(Rule{
		Host:       u.Hostname(),
		Method:     "GET",
		PathPrefix: "/v1/",
		SecretName: "api-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Also allow plain path /public without secret
	_, err = st.AddRule(Rule{
		Host:       u.Hostname(),
		PathPrefix: "/public",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(st, sec, "127.0.0.1:0", slog.Default())
	proxyHTTP := httptest.NewServer(srv.Handler())
	defer proxyHTTP.Close()

	// Deny: wrong path
	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/other", nil)
	res, err := doViaProxy(proxyHTTP.URL, "", req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("deny status %d", res.StatusCode)
	}

	// Allow with secret inject
	req, _ = http.NewRequest(http.MethodGet, upstream.URL+"/v1/x", nil)
	res, err = doViaProxy(proxyHTTP.URL, "", req)
	if err != nil {
		t.Fatal(err)
	}
	auth := res.Header.Get("X-Auth")
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("allow status %d body %s", res.StatusCode, body)
	}
	if auth != "Bearer secret-token" {
		t.Fatalf("auth inject %q", auth)
	}

	// Allow public without secret
	req, _ = http.NewRequest(http.MethodGet, upstream.URL+"/public", nil)
	res, err = doViaProxy(proxyHTTP.URL, "", req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("public status %d", res.StatusCode)
	}
}

func TestProxyClientAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)

	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.AddRule(Rule{Host: u.Hostname()})
	c, err := st.CreateClient("t")
	if err != nil {
		t.Fatal(err)
	}

	srv := NewServer(st, nil, "127.0.0.1:0", slog.Default())
	proxyHTTP := httptest.NewServer(srv.Handler())
	defer proxyHTTP.Close()

	req, _ := http.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	res, err := doViaProxy(proxyHTTP.URL, "", req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusProxyAuthRequired {
		t.Fatalf("want 407 got %d", res.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, upstream.URL+"/", nil)
	res, err = doViaProxy(proxyHTTP.URL, c.Token, req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 || string(body) != "ok" {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
}

func TestCONNECTAllowDeny(t *testing.T) {
	// TCP echo-ish: accept and close after reading nothing — just need successful dial + tunnel.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.AddRule(Rule{Host: host}) // allow only this host

	srv := NewServer(st, nil, "127.0.0.1:0", slog.Default())
	// Need real TCP listener for Hijack
	pln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pln.Close() }()
	go func() { _ = http.Serve(pln, srv.Handler()) }()

	// Deny CONNECT to other host
	conn, err := net.Dial("tcp", pln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(conn, "CONNECT evil.example.com:443 HTTP/1.1\r\nHost: evil.example.com:443\r\n\r\n")
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if !strings.Contains(status, "403") {
		t.Fatalf("deny status line %q", status)
	}

	// Allow CONNECT to upstream host
	conn, err = net.DialTimeout("tcp", pln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(conn, "CONNECT %s:%s HTTP/1.1\r\nHost: %s:%s\r\n\r\n", host, port, host, port)
	br = bufio.NewReader(conn)
	status, err = br.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "200") {
		// drain
		rest, _ := io.ReadAll(io.LimitReader(br, 512))
		t.Fatalf("allow status %q rest %s", status, rest)
	}
	// drain headers
	for {
		line, err := br.ReadString('\n')
		if err != nil || line == "\r\n" {
			break
		}
	}
	_ = conn.Close()
	_ = ln.Close()
	wg.Wait()
}

func doViaProxy(proxyURL, token string, req *http.Request) (*http.Response, error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if token != "" {
		u.User = url.User(token)
	}
	tr := &http.Transport{
		Proxy: http.ProxyURL(u),
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).DialContext,
	}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	return client.Do(req)
}

// compile-time / unused import guard
var _ = context.Background

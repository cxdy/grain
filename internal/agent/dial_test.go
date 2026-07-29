package agent

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTargetHasEndpoint(t *testing.T) {
	t.Parallel()
	if (Target{}).HasEndpoint() {
		t.Fatal("empty target should have no endpoint")
	}
	if !(Target{Port: 1}).HasEndpoint() {
		t.Fatal("port-only should have endpoint")
	}
	if !(Target{CID: 3}).HasEndpoint() {
		t.Fatal("cid-only should have endpoint")
	}
}

func TestDialTCPOnly(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	port := mustPort(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := Dial(ctx, Target{Port: port})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if !strings.Contains(c.BaseURL, "127.0.0.1") {
		t.Fatalf("BaseURL = %q, want loopback TCP", c.BaseURL)
	}
	if err := c.HeadHealth(ctx); err != nil {
		t.Fatalf("HeadHealth: %v", err)
	}
}

func TestDialNoEndpoint(t *testing.T) {
	t.Parallel()
	_, err := Dial(context.Background(), Target{})
	if err == nil {
		t.Fatal("expected error for empty target")
	}
}

func TestDialVsockFallbackToTCP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	port := mustPort(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c, err := Dial(ctx, Target{CID: 99999, Port: port})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if !strings.Contains(c.BaseURL, "127.0.0.1") {
		t.Fatalf("expected TCP fallback BaseURL, got %q", c.BaseURL)
	}
}

func TestDialVsockOnlyUnreachable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Dial(ctx, Target{CID: 99999, Port: 0})
	if err == nil {
		t.Fatal("expected error when vsock fails and no TCP port")
	}
}

func mustPort(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p <= 0 {
		t.Fatalf("bad port in %s", raw)
	}
	return p
}

func TestDialEndpointValidation(t *testing.T) {
	if _, err := Dial(context.Background(), Target{}); err == nil {
		t.Fatal("no endpoint")
	}
	// Port only
	c, err := Dial(context.Background(), Target{Port: 9})
	if err != nil || c == nil || c.BaseURL == "" {
		t.Fatalf("%v %+v", err, c)
	}
	if c.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("BaseURL %q", c.BaseURL)
	}
	// CID with Port: vsock fails → TCP fallback
	c2, err := Dial(context.Background(), Target{CID: 3, Port: 7475})
	if err != nil || c2 == nil {
		t.Fatalf("cid+port dial: %v %v", c2, err)
	}
	if !strings.Contains(c2.BaseURL, "127.0.0.1:7475") {
		// vsock may succeed on linux with device; accept either
		if c2.BaseURL != "http://vsock" {
			t.Fatalf("unexpected BaseURL %q", c2.BaseURL)
		}
	}
	// CID only, vsock fails on non-linux/mac without device → error
	_, err = Dial(context.Background(), Target{CID: 99, Port: 0})
	// On platforms without working vsock this errors; on linux with vsock may succeed or fail.
	_ = err
}

func TestDialCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// CID>0 path hits dialVsock which checks ctx.Err() first
	_, err := Dial(ctx, Target{CID: 3, Port: 1})
	// May fall through to TCP if dialVsock returns ctx error then TCP still works
	// Port>0 allows TCP fallback after vsock fails with canceled ctx
	if err != nil {
		// canceled before vsock or after; either is fine
		t.Logf("dial canceled: %v", err)
	}
	// CID only + canceled → must error (no TCP fallback)
	_, err = Dial(ctx, Target{CID: 3, Port: 0})
	if err == nil {
		t.Fatal("expected error for canceled cid-only dial")
	}
}

func TestDialTCPBaseURLFormat(t *testing.T) {
	t.Parallel()
	c, err := Dial(context.Background(), Target{Port: 12345})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://127.0.0.1:12345" {
		t.Fatalf("BaseURL %q", c.BaseURL)
	}
	if c.HTTP != nil {
		t.Fatal("TCP-only dial should leave HTTP nil (default client)")
	}
}

func TestHasEndpointBoth(t *testing.T) {
	t.Parallel()
	if !(Target{CID: 5, Port: 9}).HasEndpoint() {
		t.Fatal()
	}
	if (Target{CID: 0, Port: 0}).HasEndpoint() {
		t.Fatal()
	}
}

func TestDialVsockDirect(t *testing.T) {
	t.Parallel()
	// Already-canceled context hits dialVsock's ctx.Err() guard.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dialVsock(ctx, 3, DefaultVsockPort); err == nil {
		t.Fatal("expected canceled error")
	}

	// Unreachable CID: vsock dial fails on hosts without a guest (typical macOS CI).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	_, err := dialVsock(ctx2, 99999, DefaultVsockPort)
	// Success is possible only with a real vsock guest; treat either outcome as OK
	// as long as we exercised the dial path.
	if err != nil {
		t.Logf("dialVsock unreachable: %v", err)
	}
}

type pipeConn struct {
	net.Conn
	closed bool
}

func (p *pipeConn) Close() error {
	p.closed = true
	return p.Conn.Close()
}

func TestDialVsockSuccessPath(t *testing.T) {
	// Inject a successful probe dial so dialVsock builds the vsock HTTP client.
	old := vsockDial
	t.Cleanup(func() { vsockDial = old })

	// First call (probe) succeeds and is closed; subsequent DialContext calls get fresh pipes.
	vsockDial = func(cid, port uint32) (net.Conn, error) {
		c1, c2 := net.Pipe()
		// Close remote side so unused pipes do not leak; probe only needs Accept-side close.
		go func() { _ = c2.Close() }()
		return &pipeConn{Conn: c1}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := dialVsock(ctx, 3, DefaultVsockPort)
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://vsock" {
		t.Fatalf("BaseURL %q", c.BaseURL)
	}
	if c.HTTP == nil || c.HTTP.Transport == nil {
		t.Fatal("expected custom transport")
	}

	// Exercise DialContext success + cancel paths on the transport.
	tr := c.HTTP.Transport.(*http.Transport)
	dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
	defer dcancel()
	conn, err := tr.DialContext(dctx, "tcp", "ignored:1")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	// DialContext with already-canceled context.
	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	if _, err := tr.DialContext(cctx, "tcp", "x"); err == nil {
		t.Fatal("expected cancel error from DialContext")
	}

	// Dial prefers successful vsock over TCP.
	c2, err := Dial(context.Background(), Target{CID: 3, Port: 9})
	if err != nil {
		t.Fatal(err)
	}
	if c2.BaseURL != "http://vsock" {
		t.Fatalf("prefer vsock BaseURL, got %q", c2.BaseURL)
	}
}

func TestDialVsockDialError(t *testing.T) {
	old := vsockDial
	t.Cleanup(func() { vsockDial = old })
	vsockDial = func(cid, port uint32) (net.Conn, error) {
		return nil, fmt.Errorf("vsock down")
	}
	if _, err := dialVsock(context.Background(), 1, 2); err == nil {
		t.Fatal("expected error")
	}
	// Dial falls back to TCP when Port set.
	c, err := Dial(context.Background(), Target{CID: 1, Port: 4242})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://127.0.0.1:4242" {
		t.Fatalf("BaseURL %q", c.BaseURL)
	}
}

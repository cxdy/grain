package agent

import (
	"context"
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

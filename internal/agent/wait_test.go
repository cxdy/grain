package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitNilClient(t *testing.T) {
	t.Parallel()
	if err := Wait(context.Background(), nil); err == nil {
		t.Fatal("expected nil client error")
	}
}

func TestWaitImmediateSuccessAndCancel(t *testing.T) {
	srv := NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	var c *Client
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && len(addr) > 2 && addr[len(addr)-2:] != ":0" {
			c = &Client{BaseURL: "http://" + addr}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("no addr")
	}
	if err := Wait(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	// cancel path on dead port
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	dead := &Client{BaseURL: "http://127.0.0.1:1"}
	_ = Wait(ctx, dead)
}

func TestWaitNilHTTPClientUsesPollTimeout(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			http.Error(w, "wait", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	// HTTP nil → Wait installs short-timeout client
	c := &Client{BaseURL: srv.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Wait(ctx, c); err != nil {
		t.Fatal(err)
	}
}

func TestWaitSucceedsOnSecondPoll(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		if n.Add(1) < 2 {
			http.Error(w, "not yet", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Wait(ctx, c); err != nil {
		t.Fatal(err)
	}
	if n.Load() < 2 {
		t.Fatalf("polls=%d", n.Load())
	}
}

func TestWaitTimeoutDeadPort(t *testing.T) {
	c := &Client{
		BaseURL: "http://127.0.0.1:1",
		HTTP:    &http.Client{Timeout: 100 * time.Millisecond},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err := Wait(ctx, c)
	if err == nil {
		t.Fatal("expected Wait timeout error")
	}
}

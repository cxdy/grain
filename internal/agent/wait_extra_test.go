package agent

import (
	"context"
	"testing"
	"time"
)

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
	// wait for listen
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

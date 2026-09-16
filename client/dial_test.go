package client

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestTransientDial(t *testing.T) {
	t.Parallel()
	if transientDial(nil) {
		t.Fatal("nil")
	}
	if !transientDial(errors.New("dial tcp 192.168.4.108:7474: connect: no route to host")) {
		t.Fatal("unreach")
	}
	if !transientDial(errors.New("network is unreachable")) {
		t.Fatal("net")
	}
	if transientDial(errors.New("connection refused")) {
		t.Fatal("refused")
	}
}

func TestRetryingHTTPTransportDials(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.Close()
		}
	}()

	tr, ok := retryingHTTPTransport().(*http.Transport)
	if !ok {
		t.Fatalf("type %T", retryingHTTPTransport())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := tr.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestRetryingHTTPTransportRefused(t *testing.T) {
	t.Parallel()
	tr, ok := retryingHTTPTransport().(*http.Transport)
	if !ok {
		t.Fatal("transport")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := tr.DialContext(ctx, "tcp", "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error")
	}
}

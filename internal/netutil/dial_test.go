//go:build unix

package netutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"
)

func TestIsHostUnreachable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"refused", syscall.ECONNREFUSED, false},
		{"host", syscall.EHOSTUNREACH, true},
		{"net", syscall.ENETUNREACH, true},
		{"wrapped op", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.EHOSTUNREACH}, true},
		{"http wrapper", fmt.Errorf(`Get "http://192.168.4.108:7474/vms": %w`, &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}), true},
		{"string", errors.New("dial tcp 10.0.0.1:7474: connect: no route to host"), true},
		{"net string", errors.New("connect: network is unreachable"), true},
		{"other", errors.New("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsHostUnreachable(tt.err); got != tt.want {
				t.Fatalf("IsHostUnreachable(%v) = %v, want %v", tt.err, got, tt.want)
			}
			if got := IsTransientDialError(tt.err); got != tt.want {
				t.Fatalf("IsTransientDialError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestIsDialTimeout(t *testing.T) {
	t.Parallel()
	if IsDialTimeout(nil) {
		t.Fatal("nil")
	}
	if !IsDialTimeout(context.DeadlineExceeded) {
		t.Fatal("deadline")
	}
	if !IsDialTimeout(timeoutErr{}) {
		t.Fatal("net.Error timeout")
	}
	if IsDialTimeout(syscall.ECONNREFUSED) {
		t.Fatal("refused is not timeout")
	}
}

func TestRetryDialContextRecovers(t *testing.T) {
	oldDial := netDialContext
	oldSleep := retrySleep
	oldBack := retryBackoff
	t.Cleanup(func() {
		netDialContext = oldDial
		retrySleep = oldSleep
		retryBackoff = oldBack
	})
	retrySleep = func(time.Duration) {}
	retryBackoff = time.Millisecond

	n := 0
	netDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		n++
		if n < 3 {
			return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.EHOSTUNREACH}
		}
		c1, c2 := net.Pipe()
		t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
		return c1, nil
	}

	conn, err := RetryDialContext(context.Background(), "tcp", "192.168.4.108:7474")
	if err != nil {
		t.Fatal(err)
	}
	if conn == nil {
		t.Fatal("nil conn")
	}
	if n != 3 {
		t.Fatalf("attempts %d, want 3", n)
	}
}

func TestRetryDialContextGivesUp(t *testing.T) {
	oldDial := netDialContext
	oldSleep := retrySleep
	t.Cleanup(func() {
		netDialContext = oldDial
		retrySleep = oldSleep
	})
	retrySleep = func(time.Duration) {}

	netDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return nil, syscall.EHOSTUNREACH
	}
	_, err := RetryDialContext(context.Background(), "tcp", "10.0.0.1:1")
	if !IsHostUnreachable(err) {
		t.Fatalf("got %v", err)
	}
}

func TestRetryDialContextNoRetryOnRefused(t *testing.T) {
	oldDial := netDialContext
	t.Cleanup(func() { netDialContext = oldDial })
	n := 0
	netDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		n++
		return nil, syscall.ECONNREFUSED
	}
	_, err := RetryDialContext(context.Background(), "tcp", "127.0.0.1:1")
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("got %v", err)
	}
	if n != 1 {
		t.Fatalf("attempts %d, want 1", n)
	}
}

func TestRetryDialContextAlreadyDone(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RetryDialContext(ctx, "tcp", "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDefaultNetDialContextRefused(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := defaultNetDialContext(ctx, "tcp", "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRetryDialContextHonorsCancel(t *testing.T) {
	oldDial := netDialContext
	oldSleep := retrySleep
	t.Cleanup(func() {
		netDialContext = oldDial
		retrySleep = oldSleep
	})
	retrySleep = func(time.Duration) {}
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	netDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		n++
		cancel()
		return nil, syscall.EHOSTUNREACH
	}
	_, err := RetryDialContext(ctx, "tcp", "10.0.0.1:1")
	if err == nil {
		t.Fatal("expected error")
	}
	if n > 2 {
		t.Fatalf("kept retrying after cancel: %d", n)
	}
}

package guest

import (
	"context"
	"testing"
	"time"
)

func TestWaitSSHImmediateCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WaitSSH(ctx, "127.0.0.1", 1, "u", "")
	if err == nil {
		t.Fatal("expected cancel error")
	}
}

func TestWaitSSHShortTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := WaitSSH(ctx, "127.0.0.1", 1, "root", "/no/key")
	if err == nil {
		t.Fatal("expected timeout")
	}
}

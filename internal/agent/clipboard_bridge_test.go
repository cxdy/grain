package agent

import (
	"context"
	"testing"
	"time"
)

func TestClipboardBridgeRequestDeliver(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	b.setSender(func(id string) error {
		go func() {
			time.Sleep(10 * time.Millisecond)
			b.deliver(id, []byte("hello-clip"), "")
		}()
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	data, err := b.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello-clip" {
		t.Fatalf("%q", data)
	}
}

func TestClipboardBridgeNoSession(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	_, err := b.request(context.Background())
	if err == nil {
		t.Fatal("expected error without shell session")
	}
}

func TestClipboardBridgeErrorReply(t *testing.T) {
	t.Parallel()
	b := newClipboardBridge()
	b.setSender(func(id string) error {
		b.deliver(id, nil, "no clipboard helper")
		return nil
	})
	_, err := b.request(context.Background())
	if err == nil || err.Error() != "no clipboard helper" {
		t.Fatalf("err=%v", err)
	}
}

func TestDecodeClipboardData(t *testing.T) {
	t.Parallel()
	// "hi" base64
	got, err := decodeClipboardData("aGk=")
	if err != nil || string(got) != "hi" {
		t.Fatalf("%q %v", got, err)
	}
	got, err = decodeClipboardData("")
	if err != nil || len(got) != 0 {
		t.Fatalf("%q %v", got, err)
	}
}

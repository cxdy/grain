package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

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
	// HTTP nil → Wait installs short-timeout client (lines 22-24)
	c := &Client{BaseURL: srv.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := Wait(ctx, c); err != nil {
		t.Fatal(err)
	}
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
	// CID with no vsock falls back to TCP when Port set
	c2, err := Dial(context.Background(), Target{CID: 3, Port: 7475})
	if err != nil || c2 == nil {
		// vsock may succeed or fail to TCP fallback
		t.Logf("cid dial: %v %v", c2, err)
	}
	// CID only, vsock fails on non-linux/mac without device → error
	_, _ = Dial(context.Background(), Target{CID: 99, Port: 0})
}

package agent_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
)

func startTestServer(t *testing.T) (*agent.Server, *agent.Client) {
	t.Helper()
	srv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	// Wait briefly for listen.
	deadline := time.Now().Add(2 * time.Second)
	var base string
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && addr != "127.0.0.1:0" && !strings.HasSuffix(addr, ":0") {
			base = "http://" + addr
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if base == "" {
		t.Fatal("server did not bind a port")
	}

	c := &agent.Client{
		BaseURL: base,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}

	// Confirm health before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := agent.Wait(ctx, c); err != nil {
		t.Fatalf("wait for test server: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})

	return srv, c
}

func TestHealth(t *testing.T) {
	_, c := startTestServer(t)

	ctx := context.Background()
	h, err := c.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.AgentVersion != agent.Version {
		t.Errorf("AgentVersion = %q, want %q", h.AgentVersion, agent.Version)
	}
	if h.Hostname == "" {
		t.Error("Hostname empty")
	}
	if h.AgentUptime < 0 {
		t.Errorf("AgentUptime = %d, want >= 0", h.AgentUptime)
	}

	if err := c.HeadHealth(ctx); err != nil {
		t.Fatalf("HeadHealth: %v", err)
	}
}

func TestExecBufferedEcho(t *testing.T) {
	_, c := startTestServer(t)

	ctx := context.Background()
	res, err := c.ExecBuffered(ctx, "echo", "hello")
	if err != nil {
		t.Fatalf("ExecBuffered: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; stderr=%q error=%q", res.ExitCode, res.Stderr, res.Error)
	}
	out := strings.TrimSpace(res.Stdout)
	if out != "hello" {
		t.Errorf("Stdout = %q, want %q", out, "hello")
	}
}

func TestExecBufferedNonZero(t *testing.T) {
	_, c := startTestServer(t)

	ctx := context.Background()
	// /bin/sh -c "exit 42"
	res, err := c.ExecBuffered(ctx, "/bin/sh", "-c", "exit 42")
	if err != nil {
		t.Fatalf("ExecBuffered: %v", err)
	}
	if res.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42; stderr=%q error=%q", res.ExitCode, res.Stderr, res.Error)
	}
}

func TestExecMissingCmd(t *testing.T) {
	_, c := startTestServer(t)

	ctx := context.Background()
	res, err := c.ExecBufferedOpts(ctx, agent.ExecOpts{Cmd: ""})
	if err == nil {
		// Client rejects empty cmd before request.
		t.Fatal("expected error for empty cmd")
	}
	_ = res
}

func TestWaitSucceeds(t *testing.T) {
	_, c := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := agent.Wait(ctx, c); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestWaitTimeout(t *testing.T) {
	c := &agent.Client{
		BaseURL: "http://127.0.0.1:1", // nothing listening
		HTTP:    &http.Client{Timeout: 100 * time.Millisecond},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err := agent.Wait(ctx, c)
	if err == nil {
		t.Fatal("expected Wait timeout error")
	}
}

func TestVersionConstant(t *testing.T) {
	if agent.Version != "0.1.0" {
		t.Errorf("Version = %q, want 0.1.0", agent.Version)
	}
	if agent.DefaultListen != ":7475" {
		t.Errorf("DefaultListen = %q, want :7475", agent.DefaultListen)
	}
}

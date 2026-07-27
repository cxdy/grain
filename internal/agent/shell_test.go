package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
)

func TestShellControlResizeJSON(t *testing.T) {
	ctrl := agent.ShellControl{Type: "resize", Cols: 120, Rows: 40}
	b, err := json.Marshal(ctrl)
	if err != nil {
		t.Fatal(err)
	}
	var got agent.ShellControl
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "resize" || got.Cols != 120 || got.Rows != 40 {
		t.Fatalf("got %+v", got)
	}
	// Wire format expected by the guest handler.
	if !strings.Contains(string(b), `"type":"resize"`) {
		t.Fatalf("unexpected json: %s", b)
	}
}

func TestShellEndpoint(t *testing.T) {
	c := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Direct HTTP GET without upgrade — exercise handler path.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/shell?cols=80&rows=24", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))

	if runtime.GOOS != "linux" {
		// Stub: non-Linux builds return 501 so package tests pass on macOS CI.
		if res.StatusCode != http.StatusNotImplemented {
			t.Fatalf("non-linux shell: status %d body %q, want 501", res.StatusCode, body)
		}
		return
	}

	// On Linux a bare GET without WebSocket upgrade should fail accept (not 501).
	// 4xx/5xx both acceptable; must not be 404 (route registered).
	if res.StatusCode == http.StatusNotFound {
		t.Fatalf("shell route missing: %d %s", res.StatusCode, body)
	}
}

func TestShellClientDialNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("stub behavior is for non-linux hosts")
	}
	c := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Shell(ctx, agent.ShellOpts{
		Cols: 80, Rows: 24,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Raw:    boolPtr(false),
	})
	if err == nil {
		t.Fatal("expected error dialing stub shell on non-linux")
	}
}

func boolPtr(v bool) *bool { return &v }

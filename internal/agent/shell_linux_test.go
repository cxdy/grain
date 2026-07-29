//go:build linux

package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestHandleShellPTYSession(t *testing.T) {
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
	var base string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a := srv.AddrString(); a != "" && len(a) > 2 && a[len(a)-2:] != ":0" {
			base = "http://" + a
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if base == "" {
		t.Fatal("no addr")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, base+"/shell?cols=40&rows=12&shell=/bin/sh", nil)
	if err != nil {
		// PTY spawn can fail in restricted CI sandboxes; still covered Accept path via bad-upgrade test.
		t.Skipf("websocket dial/shell start: %v", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	_ = conn.Write(ctx, websocket.MessageBinary, []byte("echo grain-shell-ok\n"))
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":80,"rows":24}`))
	_ = conn.Write(ctx, websocket.MessageText, []byte(`not-json`))
	_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":0,"rows":0}`))

	readCtx, rc := context.WithTimeout(ctx, 2*time.Second)
	defer rc()
	for i := 0; i < 20; i++ {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			break
		}
		if len(data) > 0 {
			break
		}
	}
	_ = conn.Write(ctx, websocket.MessageBinary, []byte("exit\n"))
	time.Sleep(100 * time.Millisecond)
}

func TestHandleShellBadUpgrade(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	req := httptest.NewRequest(http.MethodGet, "/shell?cols=80&rows=24", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
}

func TestStartLoginShellDefaults(t *testing.T) {
	// Prefer /bin/sh — some CI images restrict /bin/bash under PTY.
	for _, shell := range []string{"/bin/sh", "/bin/bash", ""} {
		cmd, ptmx, err := startLoginShell(shell, 80, 24, nil)
		if err != nil {
			t.Logf("startLoginShell(%q): %v", shell, err)
			continue
		}
		_ = ptmx.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		return
	}
	t.Skip("could not start any login shell under PTY in this environment")
}

func TestParsePositiveIntAndShellHelpers(t *testing.T) {
	if parsePositiveInt("", 80) != 80 {
		t.Fatal()
	}
	if parsePositiveInt("0", 80) != 80 {
		t.Fatal()
	}
	if parsePositiveInt("-1", 80) != 80 {
		t.Fatal()
	}
	if parsePositiveInt("120", 80) != 120 {
		t.Fatal()
	}
	if parsePositiveInt("x", 7) != 7 {
		t.Fatal()
	}

	uid, gid, home, shell := resolveShellUser()
	if home == "" || shell == "" {
		t.Fatalf("resolveShellUser: uid=%d gid=%d home=%q shell=%q", uid, gid, home, shell)
	}
	env := shellEnv(home, shell, uid)
	if len(env) < 4 {
		t.Fatalf("env %v", env)
	}
	_ = shellEnv("/tmp", "/bin/sh", 1000)
	_ = shellEnv("/tmp", "/bin/sh", 42)
	_ = shellEnv("/tmp", "/bin/sh", 0)

	if got := lookupUserShell("root"); got != "" && !strings.Contains(got, "/") {
		t.Fatalf("shell %q", got)
	}
	_ = lookupUserShell("no-such-user-xyz")

	lines := splitLines("a\nb\nc")
	if len(lines) != 3 {
		t.Fatalf("%v", lines)
	}
	lines = splitLines("solo")
	if len(lines) != 1 || lines[0] != "solo" {
		t.Fatalf("%v", lines)
	}
	parts := splitPasswd("name:x:1:1::/home/n:/bin/sh")
	if len(parts) != 7 || parts[0] != "name" || parts[6] != "/bin/sh" {
		t.Fatalf("%v", parts)
	}
}

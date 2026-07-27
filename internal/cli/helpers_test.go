package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/proxy"
)

func TestPortOf(t *testing.T) {
	t.Parallel()
	if got := portOf("0.0.0.0:3128"); got != "3128" {
		t.Fatalf("got %q", got)
	}
	if got := portOf("127.0.0.1:8080"); got != "8080" {
		t.Fatalf("got %q", got)
	}
	if got := portOf("no-port"); got != fmt.Sprintf("%d", proxy.DefaultGuestProxyPort) {
		t.Fatalf("got %q", got)
	}
	if got := portOf(":bad"); got != fmt.Sprintf("%d", proxy.DefaultGuestProxyPort) {
		t.Fatalf("got %q for :bad", got)
	}
	if got := portOf(""); got != fmt.Sprintf("%d", proxy.DefaultGuestProxyPort) {
		t.Fatalf("got %q for empty", got)
	}
}

func TestReadPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pid")
	if err := os.WriteFile(path, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pid, err := readPID(path)
	if err != nil {
		t.Fatal(err)
	}
	if pid != 12345 {
		t.Fatalf("pid=%d", pid)
	}
	if _, err := readPID(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected error for missing pid")
	}
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPID(path); err == nil {
		t.Fatal("expected bad pid error")
	}
	if err := os.WriteFile(path, []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPID(path); err == nil {
		t.Fatal("expected bad pid for 0")
	}
}

func TestPidAlive(t *testing.T) {
	// Current process is alive.
	if !pidAlive(os.Getpid()) {
		t.Fatal("current pid should be alive")
	}
	// Unlikely PID that is not running (and not our process).
	if pidAlive(1<<30 - 1) {
		// On some systems FindProcess always succeeds; Signal(0) should fail.
		// If this PID happens to exist, skip rather than fail.
		t.Skip("unlikely pid is alive on this system")
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if fileExists(p) {
		t.Fatal("missing file should not exist")
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(p) {
		t.Fatal("expected file to exist")
	}
}

func TestGrainSSHIdentity(t *testing.T) {
	cfg := config.Config{DataDir: "/tmp/grain-x"}
	got := grainSSHIdentity(cfg)
	want := filepath.Join("/tmp/grain-x", "ssh", "id_grain")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSSHBaseArgs(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, SSHUser: "ubuntu"}
	// No identity file — still returns args.
	args := sshBaseArgs(cfg, "127.0.0.1", 2222)
	if len(args) == 0 {
		t.Fatal("expected ssh args")
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "2222") {
		t.Fatalf("args missing port: %v", args)
	}
	// With identity present
	idDir := filepath.Join(dir, "ssh")
	if err := os.MkdirAll(idDir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := filepath.Join(idDir, "id_grain")
	if err := os.WriteFile(id, []byte("fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	args = sshBaseArgs(cfg, "127.0.0.1", 2222)
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, id) {
		t.Fatalf("expected identity in args: %v", args)
	}
}

func TestExitCodeError(t *testing.T) {
	t.Parallel()
	e := exitCodeError(42)
	if e.Error() != "exit status 42" {
		t.Fatalf("Error()=%q", e.Error())
	}
	if e.ExitCode() != 42 {
		t.Fatalf("ExitCode()=%d", e.ExitCode())
	}
	var ec exitCodeError
	if !errors.As(e, &ec) {
		t.Fatal("errors.As failed")
	}
}

func TestResolveVMAndPath(t *testing.T) {
	t.Parallel()
	// Two-arg form does not call the client.
	name, path, err := resolveVMAndPath(nil, []string{"vm1", "/tmp/x"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "vm1" || path != "/tmp/x" {
		t.Fatalf("got %q %q", name, path)
	}
	if _, _, err := resolveVMAndPath(nil, nil); err == nil {
		t.Fatal("expected error for empty args")
	}
	if _, _, err := resolveVMAndPath(nil, []string{"a", "b", "c"}); err == nil {
		t.Fatal("expected error for three args")
	}
}

func TestIsAgentUnavailable(t *testing.T) {
	t.Parallel()
	if isAgentUnavailable(nil) {
		t.Fatal("nil")
	}
	if !isAgentUnavailable(errAgentSkip) {
		t.Fatal("errAgentSkip")
	}
	if !isAgentUnavailable(fmt.Errorf("agent not available for sbox-1")) {
		t.Fatal("substring agent not available")
	}
	if !isAgentUnavailable(fmt.Errorf("something agent skip something")) {
		t.Fatal("substring agent skip")
	}
	if isAgentUnavailable(fmt.Errorf("permission denied")) {
		t.Fatal("unrelated error")
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()
	if g := shellQuote(""); g != "''" {
		t.Fatalf("empty: %q", g)
	}
	if g := shellQuote("simple"); g != "simple" {
		t.Fatalf("simple: %q", g)
	}
	if g := shellQuote("a=b"); g != "a=b" {
		t.Fatalf("safe: %q", g)
	}
	if g := shellQuote("has space"); !strings.HasPrefix(g, "'") {
		t.Fatalf("space should quote: %q", g)
	}
	if g := shellQuote("it's"); !strings.Contains(g, `'"'"'`) {
		t.Fatalf("apostrophe: %q", g)
	}
}

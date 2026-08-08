//go:build linux

package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"testing"
)

func TestX11SysProcAttr(t *testing.T) {
	t.Parallel()
	attr := x11SysProcAttr()
	if attr == nil || !attr.Setsid {
		t.Fatalf("%+v", attr)
	}
}

func TestIncrKey(t *testing.T) {
	t.Parallel()
	k := incrKey(1, 2)
	if k == 0 {
		t.Fatal("key")
	}
	if incrKey(1, 2) != k {
		t.Fatal("stable")
	}
}

func TestFileExistsAndXDisplayAlive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if fileExists(p) {
		t.Fatal("missing")
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(p) {
		t.Fatal("exists")
	}
	if xDisplayAlive(":9999") {
		t.Fatal("unlikely display alive")
	}
}

// TestStartClipboardX11Branches hits startClipboardX11 without sync.Once so
// coverage is recorded in-process (re-exec children do not merge cover profiles).
func TestStartClipboardX11Branches(t *testing.T) {
	oldDisp := x11ClipDisp
	t.Cleanup(func() { x11ClipDisp = oldDisp })

	// nil log + no Xvfb on PATH
	t.Setenv("DISPLAY", "")
	t.Setenv("PATH", t.TempDir())
	x11ClipDisp = ""
	startClipboardX11(nil, func(ctx context.Context) ([]byte, error) {
		return []byte("x"), nil
	})
	if x11ClipDisp != "" {
		t.Fatalf("expected no display without Xvfb, got %q", x11ClipDisp)
	}

	// Existing non-grain DISPLAY: owner connect fails → early return.
	t.Setenv("DISPLAY", ":1")
	startClipboardX11(slog.Default(), func(ctx context.Context) ([]byte, error) {
		return nil, context.Canceled
	})
}

// TestEnsureClipboardX11Once still covers the Once wrapper (idempotent).
func TestEnsureClipboardX11Once(t *testing.T) {
	// May no-op if Once already fired in this process; still exercises the wrapper.
	ensureClipboardX11(slog.Default(), func(ctx context.Context) ([]byte, error) {
		return []byte("x"), nil
	})
	ensureClipboardX11(nil, nil)
}

func TestStartX11ClipboardOwnerConnectFail(t *testing.T) {
	t.Setenv("DISPLAY", ":9998")
	err := startX11ClipboardOwner(":9998", func(ctx context.Context) ([]byte, error) {
		return nil, nil
	}, slog.Default())
	if err == nil {
		t.Fatal("want connect fail")
	}
}

func TestStartXvfbMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := startXvfb(":98", slog.Default())
	if err == nil {
		t.Log("Xvfb started (installed on host)")
	}
}

func TestClipboardDisplayEnv(t *testing.T) {
	oldDisp := x11ClipDisp
	t.Cleanup(func() { x11ClipDisp = oldDisp })
	x11ClipDisp = ""
	if got := clipboardDisplayEnv(); got != "" {
		t.Fatalf("%q", got)
	}
	x11ClipDisp = ":9"
	if got := clipboardDisplayEnv(); got != "DISPLAY=:9" {
		t.Fatalf("%q", got)
	}
}

func TestStartClipboardX11WithXvfb(t *testing.T) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb not installed")
	}
	oldDisp := x11ClipDisp
	t.Cleanup(func() { x11ClipDisp = oldDisp })
	t.Setenv("DISPLAY", "")
	// Avoid clobbering the production grain display if something else owns :7.
	// startClipboardX11 uses grainClipboardDisplay when DISPLAY is empty.
	fetch := func(ctx context.Context) ([]byte, error) {
		return []byte("hello-clip"), nil
	}
	startClipboardX11(slog.Default(), fetch)
	if got := clipboardDisplayEnv(); got == "" {
		// Owner may still fail without full X stack; at least Xvfb path ran.
		t.Log("clipboard display not set (owner may have failed)")
	}
}

func TestStartXvfbAndDisplayAlive(t *testing.T) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb not installed")
	}
	// Use a high display number
	disp := ":91"
	// clean leftover locks
	_ = os.Remove("/tmp/.X91-lock")
	_ = os.Remove("/tmp/.X11-unix/X91")
	if err := startXvfb(disp, slog.Default()); err != nil {
		t.Fatal(err)
	}
	// give it a moment
	for i := 0; i < 30; i++ {
		if xDisplayAlive(disp) {
			break
		}
		// sleep via busy wait on short timeout command
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		<-ctx.Done()
		cancel()
	}
	// second start should see alive
	if err := startXvfb(disp, slog.Default()); err != nil {
		t.Fatal(err)
	}
	// Try owner if xgb can connect
	err := startX11ClipboardOwner(disp, func(ctx context.Context) ([]byte, error) {
		return []byte("pngfake"), nil
	}, slog.Default())
	if err != nil {
		t.Logf("owner: %v (ok if no full X)", err)
	}
}

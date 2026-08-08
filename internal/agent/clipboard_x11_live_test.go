//go:build linux

package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

// Live Xvfb + optional xclip paste to exercise SelectionRequest path.
func TestClipboardX11LiveOwnerAndPaste(t *testing.T) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb required")
	}
	// Isolate Once: only works if this is first call in process.
	// Use re-exec.
	if os.Getenv("GRAIN_CLIP_LIVE") != "1" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(self, "-test.run=^TestClipboardX11LiveOwnerAndPaste$", "-test.v", "-test.count=1")
		cmd.Env = append(os.Environ(), "GRAIN_CLIP_LIVE=1", "DISPLAY=", "PATH="+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatalf("%v", err)
		}
		return
	}

	var mu sync.Mutex
	payload := []byte("live-clipboard-data")
	fetch := func(ctx context.Context) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		return append([]byte(nil), payload...), nil
	}
	ensureClipboardX11(slog.Default(), fetch)
	disp := clipboardDisplayEnv()
	if disp == "" {
		// Try manual start on dedicated display
		d := ":92"
		_ = os.Remove("/tmp/.X92-lock")
		if err := startXvfb(d, slog.Default()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		var last error
		for time.Now().Before(deadline) {
			if err := startX11ClipboardOwner(d, fetch, slog.Default()); err == nil {
				x11ClipDisp = d
				last = nil
				break
			} else {
				last = err
			}
			time.Sleep(100 * time.Millisecond)
		}
		if last != nil && x11ClipDisp == "" {
			t.Fatalf("owner: %v", last)
		}
		disp = "DISPLAY=" + x11ClipDisp
	}
	t.Logf("display env %s", disp)

	// If xclip present, request clipboard to fire SelectionRequest handlers.
	if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard", "-o")
		// set DISPLAY from disp
		env := os.Environ()
		if x11ClipDisp != "" {
			env = append(env, "DISPLAY="+x11ClipDisp)
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		t.Logf("xclip: %q err=%v", out, err)
		// also request TARGETS
		cmd2 := exec.Command("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		cmd2.Env = env
		out2, _ := cmd2.CombinedOutput()
		t.Logf("targets: %q", out2)
		time.Sleep(200 * time.Millisecond)
	}
	// PNG path
	mu.Lock()
	payload = append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, 100)...)
	mu.Unlock()
	if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
		if x11ClipDisp != "" {
			cmd.Env = append(os.Environ(), "DISPLAY="+x11ClipDisp)
		}
		out, err := cmd.CombinedOutput()
		t.Logf("png paste: n=%d err=%v", len(out), err)
	}
}

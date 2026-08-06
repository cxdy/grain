//go:build live_clipboard

package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/cxdy/grain/internal/osc52"
)

// TestLiveImagePasteE2E dials a real guest agent (AGENT_URL), holds a shell
// session, and proves GET /clipboard returns a host image from ReadClipboard.
func TestLiveImagePasteE2E(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin host image paste")
	}
	agentURL := os.Getenv("AGENT_URL")
	if agentURL == "" {
		agentURL = "http://127.0.0.1:49422"
	}
	outDir := os.Getenv("CLIPBOARD_E2E_OUT")
	if outDir == "" {
		outDir = t.TempDir()
	}

	// Put a known PNG on the pasteboard (PNG + TIFF like real apps).
	pngPath := filepath.Join(outDir, "src.png")
	if err := writeSolidPNG(pngPath, 96, 72); err != nil {
		t.Fatal(err)
	}
	if err := setPasteboardImage(pngPath); err != nil {
		t.Fatalf("set pasteboard: %v", err)
	}

	hostImg, err := osc52.ReadClipboard()
	if err != nil {
		t.Fatalf("host ReadClipboard: %v", err)
	}
	if !looksLikePNGOrJPEG(hostImg) {
		t.Fatalf("host image magic %x len=%d", hostImg[:min(8, len(hostImg))], len(hostImg))
	}
	_ = os.WriteFile(filepath.Join(outDir, "host-read.png"), hostImg, 0o644)
	t.Logf("host ReadClipboard: %d bytes", len(hostImg))

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, agentURL+"/shell?cols=80&rows=24", nil)
	if err != nil {
		t.Fatalf("shell dial %s: %v", agentURL, err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(ShellWebSocketReadLimit)

	// clipboard_get handler (same as Client.Shell)
	errCh := make(chan error, 1)
	go func() {
		for {
			typ, data, rerr := conn.Read(ctx)
			if rerr != nil {
				errCh <- rerr
				return
			}
			if typ != websocket.MessageText {
				continue
			}
			var ctrl ShellControl
			if json.Unmarshal(data, &ctrl) != nil || ctrl.Type != "clipboard_get" {
				continue
			}
			reply := ShellControl{Type: "clipboard", Id: ctrl.Id}
			clip, cerr := osc52.ReadClipboard()
			if cerr != nil {
				reply.Error = cerr.Error()
			} else {
				reply.Data = base64.StdEncoding.EncodeToString(clip)
			}
			payload, _ := json.Marshal(reply)
			if werr := conn.Write(ctx, websocket.MessageText, payload); werr != nil {
				errCh <- werr
				return
			}
		}
	}()

	time.Sleep(400 * time.Millisecond)

	// Two successful pastes (plan: run live check twice).
	for attempt := 1; attempt <= 2; attempt++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, agentURL+"/clipboard", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("attempt %d GET /clipboard: %v", attempt, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Logf("attempt %d: status=%d ct=%s len=%d", attempt, resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
		if resp.StatusCode != 200 {
			t.Fatalf("attempt %d: status %d body %q", attempt, resp.StatusCode, truncate(body, 200))
		}
		if !looksLikePNGOrJPEG(body) {
			t.Fatalf("attempt %d: not image magic %x", attempt, body[:min(8, len(body))])
		}
		if len(body) < 50 {
			t.Fatalf("attempt %d: image too small (%d)", attempt, len(body))
		}
		_ = os.WriteFile(filepath.Join(outDir, fmt.Sprintf("guest-paste-%d.bin", attempt)), body, 0o644)
	}
}

func looksLikePNGOrJPEG(b []byte) bool {
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return true
	}
	return len(b) >= 2 && b[0] == 0xff && b[1] == 0xd8
}

func setPasteboardImage(pngPath string) error {
	src := `
import AppKit
let url = URL(fileURLWithPath: "` + pngPath + `")
guard let data = try? Data(contentsOf: url) else { exit(2) }
let pb = NSPasteboard.general
pb.clearContents()
pb.setData(data, forType: .png)
if let img = NSImage(data: data), let tiff = img.tiffRepresentation {
  pb.setData(tiff, forType: .tiff)
}
`
	cmd := exec.Command("swift", "-e", src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func writeSolidPNG(path string, w, h int) error {
	// Prefer python for a real PNG without external deps in Go test helpers.
	py := `
import struct,zlib,sys
w,h=int(sys.argv[1]),int(sys.argv[2])
path=sys.argv[3]
def chunk(tag,data):
    return struct.pack('>I',len(data))+tag+data+struct.pack('>I',zlib.crc32(tag+data)&0xffffffff)
rows=b''.join(b'\x00'+bytes([200,40,80,255])*w for _ in range(h))
ihdr=struct.pack('>IIBBBBB',w,h,8,6,0,0,0)
png=b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',ihdr)+chunk(b'IDAT',zlib.compress(rows,9))+chunk(b'IEND',b'')
open(path,'wb').write(png)
`
	cmd := exec.Command("python3", "-c", py, fmt.Sprint(w), fmt.Sprint(h), path)
	return cmd.Run()
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

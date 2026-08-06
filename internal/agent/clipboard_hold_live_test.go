//go:build live_clipboard_hold

package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/cxdy/grain/internal/osc52"
)

func TestHoldShellClipboard(t *testing.T) {
	agentURL := os.Getenv("AGENT_URL")
	if agentURL == "" {
		agentURL = "http://127.0.0.1:49422"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, agentURL+"/shell?cols=80&rows=24", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(ShellWebSocketReadLimit)
	t.Log("holding shell")
	for {
		typ, data, rerr := conn.Read(ctx)
		if rerr != nil {
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
		if clip, err := osc52.ReadClipboard(); err != nil {
			reply.Error = err.Error()
		} else {
			reply.Data = base64.StdEncoding.EncodeToString(clip)
			t.Logf("served %d", len(clip))
		}
		payload, _ := json.Marshal(reply)
		_ = conn.Write(ctx, websocket.MessageText, payload)
	}
}

package hypervisor

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func mockQMPServer(t *testing.T, sock string, onCmd func(cmd string) map[string]any) func() {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				enc := json.NewEncoder(c)
				dec := json.NewDecoder(c)
				_ = enc.Encode(map[string]any{"QMP": map[string]any{"version": map[string]any{}, "capabilities": []any{}}})
				for {
					var req map[string]any
					if err := dec.Decode(&req); err != nil {
						return
					}
					cmd, _ := req["execute"].(string)
					resp := map[string]any{"return": map[string]any{}}
					if onCmd != nil {
						if r := onCmd(cmd); r != nil {
							resp = r
						}
					}
					_ = enc.Encode(resp)
				}
			}(conn)
		}
	}()
	return func() {
		close(done)
		_ = ln.Close()
		wg.Wait()
	}
}

func TestQMPPathFor(t *testing.T) {
	t.Parallel()
	if QMPPathFor(nil) != "" {
		t.Fatal()
	}
	if got := QMPPathFor(&vm.Instance{QMPPath: "/x"}); got != "/x" {
		t.Fatal(got)
	}
	want := filepath.Join("/data/vms/foo", QMPSocketName)
	if got := QMPPathFor(&vm.Instance{DiskPath: "/data/vms/foo/disk.qcow2"}); got != want {
		t.Fatalf("%s != %s", got, want)
	}
}

func TestQMPNegotiateAndCommands(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(t.TempDir(), "qmp.sock")
	var mu sync.Mutex
	var cmds []string
	cleanup := mockQMPServer(t, sock, func(cmd string) map[string]any {
		mu.Lock()
		cmds = append(cmds, cmd)
		mu.Unlock()
		return map[string]any{"return": map[string]any{}}
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := DialQMP(ctx, sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Powerdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Cont(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Quit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.HumanMonitorCommand(ctx, "savevm grain-suspend"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	// first is qmp_capabilities from Dial, then powerdown/stop/cont/quit/hmp
	want := []string{"qmp_capabilities", "system_powerdown", "stop", "cont", "quit", "human-monitor-command"}
	if len(cmds) != len(want) {
		t.Fatalf("cmds %v want %v", cmds, want)
	}
	for i := range want {
		if cmds[i] != want[i] {
			t.Fatalf("cmds %v want %v", cmds, want)
		}
	}
}

func TestQMPCommandError(t *testing.T) {
	t.Parallel()
	sock := filepath.Join(t.TempDir(), "qmp.sock")
	cleanup := mockQMPServer(t, sock, func(cmd string) map[string]any {
		if cmd == "stop" {
			return map[string]any{"error": map[string]any{"class": "GenericError", "desc": "already stopped"}}
		}
		return map[string]any{"return": map[string]any{}}
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := qmpCommand(ctx, sock, "stop")
	if err == nil || !strings.Contains(err.Error(), "already stopped") {
		t.Fatalf("%v", err)
	}
}

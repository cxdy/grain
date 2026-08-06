package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/cxdy/grain/internal/desktop"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// shellBridge holds an open PTY websocket for the UI.
type shellBridge struct {
	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	vm     string
}

var activeShell shellBridge

// ShellAttach dials the daemon agent shell WebSocket and streams output to the
// frontend via EventsEmit("shell:data"). Input via ShellWrite.
// Safe to call repeatedly — replaces any previous session.
func (a *App) ShellAttach(vm string, cols, rows int) error {
	svc, err := a.service()
	if err != nil {
		return err
	}
	info, err := svc.ShellSession(vm, cols, rows)
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Close previous session fully before dialing.
	a.ShellClose()

	sctx, cancel := context.WithCancel(ctx)
	conn, err := desktop.DefaultShellDial(sctx, info)
	if err != nil {
		cancel()
		return fmt.Errorf("shell dial: %w", err)
	}
	activeShell.mu.Lock()
	activeShell.conn = conn
	activeShell.cancel = cancel
	activeShell.vm = vm
	activeShell.mu.Unlock()

	go func() {
		defer func() {
			activeShell.mu.Lock()
			if activeShell.conn == conn {
				activeShell.conn = nil
				activeShell.cancel = nil
				activeShell.vm = ""
			}
			activeShell.mu.Unlock()
		}()
		for {
			_, data, rerr := conn.Read(sctx)
			if rerr != nil {
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "shell:close", map[string]string{
						"vm":    vm,
						"error": rerr.Error(),
					})
				}
				return
			}
			if a.ctx != nil && len(data) > 0 {
				runtime.EventsEmit(a.ctx, "shell:data", map[string]string{
					"vm":   vm,
					"data": string(data),
				})
			}
		}
	}()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "shell:open", map[string]string{"vm": vm})
	}
	return nil
}

// ShellWrite sends bytes to the active shell session.
func (a *App) ShellWrite(data string) error {
	activeShell.mu.Lock()
	conn := activeShell.conn
	activeShell.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("no active shell")
	}
	ctx := a.ctx
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}
	return conn.Write(ctx, websocket.MessageBinary, []byte(data))
}

// ShellClose closes the active shell session.
func (a *App) ShellClose() error {
	activeShell.mu.Lock()
	cancel := activeShell.cancel
	conn := activeShell.conn
	activeShell.cancel = nil
	activeShell.conn = nil
	activeShell.vm = ""
	activeShell.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		return conn.Close(websocket.StatusNormalClosure, "ui")
	}
	return nil
}

// ShellActiveVM returns the VM currently attached, if any.
func (a *App) ShellActiveVM() string {
	activeShell.mu.Lock()
	defer activeShell.mu.Unlock()
	return activeShell.vm
}

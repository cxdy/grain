//go:build linux

package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync"
	"syscall"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

// handleShell upgrades to a WebSocket and bridges an interactive PTY shell.
// Query: cols, rows, shell (optional).
func (s *Server) handleShell(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cols := parsePositiveInt(q.Get("cols"), 80)
	rows := parsePositiveInt(q.Get("rows"), 24)
	shellPath := q.Get("shell")

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Agent is loopback/hostfwd only; allow any Origin for CLI clients.
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		s.Log.Error("shell websocket accept", "err", err)
		return
	}
	// Close status set by bridge; ensure cleanup on early return.
	defer conn.CloseNow()

	ctx := r.Context()
	cmd, ptmx, err := startLoginShell(shellPath, cols, rows)
	if err != nil {
		s.Log.Error("shell pty start", "err", err)
		_ = conn.Close(websocket.StatusInternalError, err.Error())
		return
	}

	var waitOnce sync.Once
	var waitErr error
	waitCmd := func() error {
		waitOnce.Do(func() {
			waitErr = cmd.Wait()
		})
		return waitErr
	}
	defer func() {
		_ = ptmx.Close()
		// Kill if still running, then reap exactly once.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = waitCmd()
	}()

	s.Log.Info("shell session started",
		"pid", cmd.Process.Pid,
		"cols", cols,
		"rows", rows,
		"shell", shellPath,
	)

	// Bridge: WS binary ↔ PTY; WS text JSON resize → pty.Setsize.
	// On PTY exit, close the WebSocket.
	errCh := make(chan error, 3)

	// PTY → WebSocket
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := ptmx.Read(buf)
			if n > 0 {
				werr := conn.Write(ctx, websocket.MessageBinary, buf[:n])
				if werr != nil {
					errCh <- werr
					return
				}
			}
			if readErr != nil {
				errCh <- readErr
				return
			}
		}
	}()

	// WebSocket → PTY (and resize control)
	go func() {
		for {
			typ, data, rerr := conn.Read(ctx)
			if rerr != nil {
				errCh <- rerr
				return
			}
			switch typ {
			case websocket.MessageBinary:
				if _, werr := ptmx.Write(data); werr != nil {
					errCh <- werr
					return
				}
			case websocket.MessageText:
				var ctrl ShellControl
				if jerr := json.Unmarshal(data, &ctrl); jerr != nil {
					continue
				}
				if ctrl.Type == "resize" && ctrl.Cols > 0 && ctrl.Rows > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Cols: uint16(ctrl.Cols),
						Rows: uint16(ctrl.Rows),
					})
				}
			}
		}
	}()

	// Wait for process exit.
	go func() {
		if werr := waitCmd(); werr != nil {
			errCh <- werr
			return
		}
		errCh <- io.EOF
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close(websocket.StatusGoingAway, "context canceled")
	case e := <-errCh:
		// Prefer normal close when PTY/process ended cleanly.
		if e == nil || e == io.EOF {
			_ = conn.Close(websocket.StatusNormalClosure, "shell exited")
		} else {
			_ = conn.Close(websocket.StatusNormalClosure, "session end")
		}
	}
}

// startLoginShell spawns a login shell in a PTY as uid 1000 when possible, else root.
func startLoginShell(shellPath string, cols, rows int) (*exec.Cmd, *os.File, error) {
	uid, gid, home, shell := resolveShellUser()
	if shellPath != "" {
		shell = shellPath
	}
	if shell == "" {
		shell = "/bin/bash"
	}

	// Login shell: argv0 with leading "-" is the traditional convention.
	cmd := exec.Command(shell, "-l")
	cmd.Dir = home
	cmd.Env = shellEnv(home, shell, uid)
	if os.Geteuid() == 0 && (uid != 0 || gid != 0) {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: uint32(uid),
				Gid: uint32(gid),
			},
			Setsid: true,
		}
	} else {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("pty start: %w", err)
	}
	return cmd, ptmx, nil
}

// resolveShellUser prefers uid 1000 (typical cloud image user), else root.
func resolveShellUser() (uid, gid int, home, shell string) {
	// Try uid 1000 first.
	if u, err := user.LookupId("1000"); err == nil {
		uid = 1000
		if g, err := strconv.Atoi(u.Gid); err == nil {
			gid = g
		}
		home = u.HomeDir
		if home == "" {
			home = "/home/" + u.Username
		}
		shell = lookupUserShell(u.Username)
		if shell == "" {
			shell = "/bin/bash"
		}
		return uid, gid, home, shell
	}
	// Fall back to current user / root.
	uid = os.Geteuid()
	gid = os.Getegid()
	home = os.Getenv("HOME")
	if home == "" {
		if uid == 0 {
			home = "/root"
		} else {
			home = "/"
		}
	}
	shell = os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	return uid, gid, home, shell
}

func lookupUserShell(username string) string {
	// Best-effort parse of /etc/passwd for the shell field.
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return ""
	}
	for _, line := range splitLines(string(data)) {
		// name:x:uid:gid:gecos:home:shell
		parts := splitPasswd(line)
		if len(parts) < 7 {
			continue
		}
		if parts[0] == username {
			return parts[6]
		}
	}
	return ""
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func splitPasswd(line string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			parts = append(parts, line[start:i])
			start = i + 1
		}
	}
	parts = append(parts, line[start:])
	return parts
}

func shellEnv(home, shell string, uid int) []string {
	// Minimal sane environment for an interactive login-like session.
	path := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	userName := "root"
	if uid == 1000 {
		if u, err := user.LookupId("1000"); err == nil {
			userName = u.Username
		} else {
			userName = "ubuntu"
		}
	} else if uid != 0 {
		if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
			userName = u.Username
		}
	}
	return []string{
		"HOME=" + home,
		"USER=" + userName,
		"LOGNAME=" + userName,
		"SHELL=" + shell,
		"TERM=xterm-256color",
		"PATH=" + path,
		"LANG=C.UTF-8",
	}
}

func parsePositiveInt(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

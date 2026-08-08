package manager

import (
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// startTCPProxy listens on 127.0.0.1:hostPort and forwards to guestIP:guestPort.
// Prefers socat; falls back to python3. Returns the proxy process PID for killPID.
func startTCPProxy(hostPort int, guestIP string, guestPort int) (int, error) {
	if hostPort <= 0 || guestPort <= 0 || guestIP == "" {
		return 0, fmt.Errorf("invalid proxy endpoints host=%d guest=%s:%d", hostPort, guestIP, guestPort)
	}
	if path, err := exec.LookPath("socat"); err == nil {
		listen := fmt.Sprintf("TCP-LISTEN:%d,bind=127.0.0.1,fork,reuseaddr", hostPort)
		remote := fmt.Sprintf("TCP:%s:%d", guestIP, guestPort)
		return startDetached(path, listen, remote)
	}
	if path, err := exec.LookPath("python3"); err == nil {
		script := `
import socket, threading, sys
hp, gip, gp = int(sys.argv[1]), sys.argv[2], int(sys.argv[3])
s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", hp))
s.listen(32)
def pipe(a,b):
    try:
        while True:
            d=a.recv(65536)
            if not d: break
            b.sendall(d)
    except Exception:
        pass
    finally:
        try: a.close()
        except Exception: pass
        try: b.close()
        except Exception: pass
while True:
    c, _ = s.accept()
    try:
        r = socket.create_connection((gip, gp), timeout=10)
    except Exception:
        c.close()
        continue
    threading.Thread(target=pipe, args=(c,r), daemon=True).start()
    threading.Thread(target=pipe, args=(r,c), daemon=True).start()
`
		return startDetached(path, "-c", script, strconv.Itoa(hostPort), guestIP, strconv.Itoa(guestPort))
	}
	return 0, fmt.Errorf("live Firecracker forward needs socat or python3 on PATH")
}

func startDetached(bin string, args ...string) (int, error) {
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// Wait briefly for early exit. Zombies still accept Signal(0), so do not
	// use kill(0) alone — prefer Wait with a short timeout.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			err = fmt.Errorf("exit 0")
		}
		return 0, fmt.Errorf("proxy died: %w", err)
	case <-time.After(120 * time.Millisecond):
		return cmd.Process.Pid, nil
	}
}

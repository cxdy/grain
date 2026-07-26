package guest

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"
)

// WaitSSH blocks until SSH accepts connections and an optional command succeeds.
func WaitSSH(ctx context.Context, host string, port int, user, privKey string) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	deadline, ok := ctx.Deadline()
	if !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wait ssh %s: %w", addr, err)
		}
		d := net.Dialer{Timeout: 2 * time.Second}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			_ = conn.Close()
			// probe with ssh
			if privKey != "" {
				if err := sshProbe(ctx, host, port, user, privKey); err == nil {
					return nil
				}
			} else {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for ssh on %s", addr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

func sshProbe(ctx context.Context, host string, port int, user, privKey string) error {
	args := []string{
		"-i", privKey,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=3",
		"-p", fmt.Sprintf("%d", port),
		fmt.Sprintf("%s@%s", user, host),
		"--", "true",
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

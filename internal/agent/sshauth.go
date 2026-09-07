package agent

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// ValidateClientSSHAuthSock reports whether path is a usable SSH agent socket.
// Empty, missing, or non-dialable paths fail; the shell must not start.
func ValidateClientSSHAuthSock(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("SSH_AUTH_SOCK is not set; -A requires a usable client SSH agent")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("SSH_AUTH_SOCK %s: %w", path, err)
	}
	if st.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("SSH_AUTH_SOCK %s is not a unix socket", path)
	}
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return fmt.Errorf("SSH_AUTH_SOCK %s is not accepting connections: %w", path, err)
	}
	_ = c.Close()
	return nil
}

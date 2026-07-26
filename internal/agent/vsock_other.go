//go:build !linux

package agent

import (
	"fmt"
	"net"
	"runtime"
)

// listenVsock is a stub on non-Linux hosts (guest agent is Linux-only for vsock).
func listenVsock(port uint32) (net.Listener, error) {
	return nil, fmt.Errorf("vsock listen not supported on %s", runtime.GOOS)
}

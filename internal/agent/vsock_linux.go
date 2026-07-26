//go:build linux

package agent

import (
	"net"

	"github.com/mdlayher/vsock"
)

// listenVsock opens an AF_VSOCK listener on the given port (guest side).
func listenVsock(port uint32) (net.Listener, error) {
	return vsock.Listen(port, nil)
}

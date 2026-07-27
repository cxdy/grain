package netutil

import (
	"fmt"
	"net"
)

// FreeTCPPort binds :0 and returns the chosen port.
func FreeTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("not tcp")
	}
	return addr.Port, nil
}

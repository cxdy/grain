package netutil

import (
	"fmt"
	"net"
)

// listenTCP is the listen hook used by FreeTCPPort (overridable in tests).
var listenTCP = func() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

// FreeTCPPort binds :0 and returns the chosen port.
func FreeTCPPort() (int, error) {
	l, err := listenTCP()
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

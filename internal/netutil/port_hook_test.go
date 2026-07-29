package netutil

import (
	"fmt"
	"net"
	"testing"
)

type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "not-tcp" }

type fakeListener struct {
	addr net.Addr
}

func (f *fakeListener) Accept() (net.Conn, error) { return nil, fmt.Errorf("no") }
func (f *fakeListener) Close() error              { return nil }
func (f *fakeListener) Addr() net.Addr            { return f.addr }

func TestFreeTCPPortListenFails(t *testing.T) {
	old := listenTCP
	t.Cleanup(func() { listenTCP = old })
	listenTCP = func() (net.Listener, error) {
		return nil, fmt.Errorf("bind failed")
	}
	if _, err := FreeTCPPort(); err == nil {
		t.Fatal("expected error")
	}
}

func TestFreeTCPPortNotTCPAddr(t *testing.T) {
	old := listenTCP
	t.Cleanup(func() { listenTCP = old })
	listenTCP = func() (net.Listener, error) {
		return &fakeListener{addr: fakeAddr{}}, nil
	}
	if _, err := FreeTCPPort(); err == nil {
		t.Fatal("expected not tcp error")
	}
}

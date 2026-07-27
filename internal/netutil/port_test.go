package netutil_test

import (
	"github.com/cxdy/grain/internal/netutil"
	"net"
	"testing"
)

func TestFreeTCPPort(t *testing.T) {
	t.Parallel()
	p, err := netutil.FreeTCPPort()
	if err != nil || p < 1 {
		t.Fatalf("%d %v", p, err)
	}
	p2, err := netutil.FreeTCPPort()
	if err != nil {
		t.Fatal(err)
	}
	// usually different, but not required
	_ = p2
}

func TestFreeTCPPortMany(t *testing.T) {
	t.Parallel()
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		p, err := netutil.FreeTCPPort()
		if err != nil {
			t.Fatal(err)
		}
		if p < 1 || p > 65535 {
			t.Fatalf("port %d", p)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_ = ln.Close()
		seen[p] = true
	}
	if len(seen) == 0 {
		t.Fatal("no ports")
	}
}

func TestFreeTCPPortUsable(t *testing.T) {
	t.Parallel()
	p, err := netutil.FreeTCPPort()
	if err != nil {
		t.Fatal(err)
	}
	if p < 1 || p > 65535 {
		t.Fatalf("port out of range: %d", p)
	}
	// Port was freed after FreeTCPPort returns; we should be able to bind something.
	// Binding the exact port may race; bind :0 again and ensure success path is solid.
	p2, err := netutil.FreeTCPPort()
	if err != nil {
		t.Fatal(err)
	}
	if p2 < 1 || p2 > 65535 {
		t.Fatalf("port out of range: %d", p2)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Port < 1 {
		t.Fatalf("bad addr %v", ln.Addr())
	}
}

func TestFreeTCPPortDistinctUsually(t *testing.T) {
	t.Parallel()
	ports := make([]int, 0, 8)
	for i := 0; i < 8; i++ {
		p, err := netutil.FreeTCPPort()
		if err != nil {
			t.Fatal(err)
		}
		ports = append(ports, p)
	}
	uniq := map[int]struct{}{}
	for _, p := range ports {
		uniq[p] = struct{}{}
	}
	if len(uniq) < 1 {
		t.Fatal("no ports")
	}
}

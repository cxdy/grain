package netutil

import (
	"net"
	"testing"
)

func TestFreeTCPPortMany(t *testing.T) {
	t.Parallel()
	seen := map[int]bool{}
	for i := 0; i < 5; i++ {
		p, err := FreeTCPPort()
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

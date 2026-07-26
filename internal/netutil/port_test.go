package netutil_test

import (
	"testing"

	"github.com/cxdy/grain/internal/netutil"
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

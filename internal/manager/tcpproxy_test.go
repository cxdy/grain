package manager

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func TestFCCreateTimePublishSpecs(t *testing.T) {
	t.Parallel()
	fwds := []vm.PortForward{
		{HostPort: 18080, GuestPort: 8080},
		{HostPort: 0, GuestPort: 90},                  // skipped
		{HostPort: 5353, GuestPort: 53, Proto: "udp"}, // skipped (tcp proxy only)
		{HostPort: 19000, GuestPort: 9000},
	}
	live := []vm.LiveForward{{HostPort: 19000, GuestPort: 9000, PID: 1}}
	got := FCCreateTimePublishSpecs(fwds, live)
	if len(got) != 1 || got[0].HostPort != 18080 || got[0].GuestPort != 8080 {
		t.Fatalf("got %+v", got)
	}
	// All covered → empty.
	if n := FCCreateTimePublishSpecs(fwds[:1], []vm.LiveForward{{HostPort: 18080, GuestPort: 8080}}); len(n) != 0 {
		t.Fatalf("expected empty, got %+v", n)
	}
}

func TestStartTCPProxyRoundTrip(t *testing.T) {
	// Backend that echoes one line.
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	_, backendPortStr, _ := net.SplitHostPort(backend.Addr().String())
	backendPort, _ := strconv.Atoi(backendPortStr)

	go func() {
		c, err := backend.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		_, _ = c.Write(append([]byte("echo:"), buf[:n]...))
	}()

	// Free host port for proxy.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, hostPortStr, _ := net.SplitHostPort(l.Addr().String())
	hostPort, _ := strconv.Atoi(hostPortStr)
	_ = l.Close()

	pid, err := startTCPProxy(hostPort, "127.0.0.1", backendPort)
	if err != nil {
		t.Skipf("proxy unavailable in this env: %v", err)
	}
	defer func() { _ = killPID(pid) }()

	deadline := time.Now().Add(3 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+hostPortStr, 200*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("hi"))
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil && len(got) == 0 {
		t.Fatal(err)
	}
	if string(got) != "echo:hi" {
		t.Fatalf("got %q", got)
	}
}

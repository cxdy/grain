package cli

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestParsePublishFlagHostGuest(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag("8080:80")
	if err != nil {
		t.Fatal(err)
	}
	if f.HostPort != 8080 || f.GuestPort != 80 {
		t.Fatalf("got %+v", f)
	}
	if f.Proto != "tcp" {
		t.Fatalf("proto %s", f.Proto)
	}
}

func TestParsePublishFlagGuestOnly(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag("80")
	if err != nil {
		t.Fatal(err)
	}
	if f.HostPort != 0 || f.GuestPort != 80 {
		t.Fatalf("got %+v", f)
	}
}

func TestParsePublishFlagColonGuestOnly(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag(":443")
	if err != nil {
		t.Fatal(err)
	}
	if f.HostPort != 0 || f.GuestPort != 443 {
		t.Fatalf("got %+v", f)
	}
}

func TestParsePublishFlagProto(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag("udp/5353:53")
	if err != nil {
		t.Fatal(err)
	}
	if f.Proto != "udp" || f.HostPort != 5353 || f.GuestPort != 53 {
		t.Fatalf("got %+v", f)
	}
}

func TestParsePublishFlagPrivilegedHost(t *testing.T) {
	t.Parallel()
	if _, err := parsePublishFlag("80:80"); err == nil {
		t.Fatal("expected privileged host port error")
	}
	if _, err := parsePublishFlag("443:443"); err == nil {
		t.Fatal("expected privileged host port error")
	}
}

func TestParsePublishFlagInvalid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "abc", "1:2:3", "0:0", "-1:80"} {
		if _, err := parsePublishFlag(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestParsePublishFlagsMultiple(t *testing.T) {
	t.Parallel()
	fwds, err := parsePublishFlags([]string{"8080:80", "90"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fwds) != 2 {
		t.Fatalf("len %d", len(fwds))
	}
	if fwds[0].HostPort != 8080 || fwds[0].GuestPort != 80 {
		t.Fatalf("0: %+v", fwds[0])
	}
	if fwds[1].HostPort != 0 || fwds[1].GuestPort != 90 {
		t.Fatalf("1: %+v", fwds[1])
	}
}

func TestParsePublishFlagsEmpty(t *testing.T) {
	t.Parallel()
	fwds, err := parsePublishFlags(nil)
	if err != nil || fwds != nil {
		t.Fatalf("got %v %v", fwds, err)
	}
}

func TestFormatSSHTunnelLine(t *testing.T) {
	t.Parallel()
	got := formatSSHTunnelLine(3000, "alice@sandbox.example.com")
	want := "ssh -N -L 3000:127.0.0.1:3000 alice@sandbox.example.com"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = formatSSHTunnelLine(8080, "USER@HOST")
	want = "ssh -N -L 8080:127.0.0.1:8080 USER@HOST"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSSHTunnelTarget(t *testing.T) {
	t.Setenv("GRAIN_SSH_HOST", "")

	if got := sshTunnelTarget("", ""); got != "USER@HOST" {
		t.Fatalf("empty: %q", got)
	}
	if got := sshTunnelTarget("alice", ""); got != "alice@HOST" {
		t.Fatalf("user only: %q", got)
	}
	if got := sshTunnelTarget("", "sandbox"); got != "sandbox" {
		t.Fatalf("host only: %q", got)
	}
	if got := sshTunnelTarget("alice", "sandbox"); got != "alice@sandbox" {
		t.Fatalf("both: %q", got)
	}

	t.Setenv("GRAIN_SSH_HOST", "env-host")
	if got := sshTunnelTarget("bob", ""); got != "bob@env-host" {
		t.Fatalf("env host: %q", got)
	}
	// explicit --host wins over env
	if got := sshTunnelTarget("bob", "flag-host"); got != "bob@flag-host" {
		t.Fatalf("flag host: %q", got)
	}
}

func TestCollectHostForwardPorts(t *testing.T) {
	t.Parallel()
	inst := &vm.Instance{
		Name: "web",
		Forwards: []vm.PortForward{
			{HostPort: 3000, GuestPort: 3000, Proto: "tcp"},
			{HostPort: 0, GuestPort: 80},                  // unallocated — skip
			{HostPort: 5353, GuestPort: 53, Proto: "udp"}, // non-tcp — skip
			{HostPort: 8443, GuestPort: 443},
		},
		LiveForwards: []vm.LiveForward{
			{HostPort: 3000, GuestPort: 3000}, // dup host — skip
			{HostPort: 9090, GuestPort: 9090},
		},
	}
	ports := collectHostForwardPorts(inst)
	if len(ports) != 3 {
		t.Fatalf("len=%d %+v", len(ports), ports)
	}
	if ports[0].HostPort != 3000 || ports[0].Kind != "slirp" {
		t.Fatalf("0: %+v", ports[0])
	}
	if ports[1].HostPort != 8443 || ports[1].Kind != "slirp" {
		t.Fatalf("1: %+v", ports[1])
	}
	if ports[2].HostPort != 9090 || ports[2].Kind != "live" {
		t.Fatalf("2: %+v", ports[2])
	}
	if collectHostForwardPorts(nil) != nil {
		t.Fatal("nil instance")
	}
	if collectHostForwardPorts(&vm.Instance{}) != nil {
		t.Fatal("empty instance")
	}
}

func TestBuildTunnelLines(t *testing.T) {
	t.Parallel()
	inst := &vm.Instance{
		Name: "web",
		Forwards: []vm.PortForward{
			{HostPort: 3000, GuestPort: 3000},
		},
		LiveForwards: []vm.LiveForward{
			{HostPort: 8080, GuestPort: 80},
		},
	}
	lines := buildTunnelLines(inst, "alice@host")
	if len(lines) != 2 {
		t.Fatalf("len=%d", len(lines))
	}
	if lines[0].Name != "web" || lines[0].HostPort != 3000 || lines[0].Kind != "slirp" {
		t.Fatalf("0: %+v", lines[0])
	}
	if !strings.Contains(lines[0].SSH, "3000:127.0.0.1:3000") || !strings.HasSuffix(lines[0].SSH, "alice@host") {
		t.Fatalf("ssh0: %q", lines[0].SSH)
	}
	if lines[1].Kind != "live" || lines[1].GuestPort != 80 {
		t.Fatalf("1: %+v", lines[1])
	}
	want := "ssh -N -L 8080:127.0.0.1:8080 alice@host"
	if lines[1].SSH != want {
		t.Fatalf("ssh1: %q want %q", lines[1].SSH, want)
	}
	if buildTunnelLines(&vm.Instance{Name: "empty"}, "x") != nil {
		t.Fatal("expected nil for no ports")
	}
}

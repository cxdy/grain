package hypervisor

import (
	"fmt"
	"strings"

	"github.com/cxdy/grain/internal/netutil"
	"github.com/cxdy/grain/internal/vm"
)

// buildUserNetdev builds a QEMU SLIRP user netdev string with SSH hostfwd plus
// any extra PortForward entries. Always forwards guest :22 to sshPort on loopback.
func buildUserNetdev(sshPort int, fwds []vm.PortForward) string {
	var b strings.Builder
	b.WriteString("user,id=net0")
	b.WriteString(fmt.Sprintf(",hostfwd=tcp:127.0.0.1:%d-:22", sshPort))
	for _, f := range fwds {
		if f.GuestPort <= 0 || f.HostPort <= 0 {
			continue
		}
		proto := f.Proto
		if proto == "" {
			proto = "tcp"
		}
		b.WriteString(fmt.Sprintf(",hostfwd=%s:127.0.0.1:%d-:%d", proto, f.HostPort, f.GuestPort))
	}
	return b.String()
}

// AllocateForwardPorts fills HostPort == 0 with free TCP ports and validates
// that no explicit host port is privileged (< 1024). Mutates fwds in place.
func AllocateForwardPorts(fwds []vm.PortForward) error {
	for i := range fwds {
		if fwds[i].HostPort == 0 {
			p, err := netutil.FreeTCPPort()
			if err != nil {
				return fmt.Errorf("allocate host port for guest %d: %w", fwds[i].GuestPort, err)
			}
			fwds[i].HostPort = p
			continue
		}
		if fwds[i].HostPort < 1024 {
			return fmt.Errorf("host port %d is privileged (< 1024); use 0 to auto-allocate or pick a port >= 1024", fwds[i].HostPort)
		}
	}
	return nil
}

// ValidateForwards checks guest ports and privileged host ports without allocating.
func ValidateForwards(fwds []vm.PortForward) error {
	for _, f := range fwds {
		if f.GuestPort <= 0 || f.GuestPort > 65535 {
			return fmt.Errorf("invalid guest port %d", f.GuestPort)
		}
		if f.HostPort < 0 || f.HostPort > 65535 {
			return fmt.Errorf("invalid host port %d", f.HostPort)
		}
		if f.HostPort > 0 && f.HostPort < 1024 {
			return fmt.Errorf("host port %d is privileged (< 1024); use 0 to auto-allocate or pick a port >= 1024", f.HostPort)
		}
		if f.Proto != "" && f.Proto != "tcp" && f.Proto != "udp" {
			return fmt.Errorf("unsupported proto %q (want tcp or udp)", f.Proto)
		}
	}
	return nil
}

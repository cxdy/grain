package hypervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cxdy/grain/internal/vm"
)

// FC network model (vFC-2, single-tenant):
//
//   - One TAP per VM (host_dev_name), attached as Firecracker network-interfaces.
//   - Host TAP address 10.77.<slot>.1/24, guest 10.77.<slot>.2/24 (static).
//   - Guest IP configured after agent is up (vsock exec) so bake need not change.
//   - Create-time publish: iptables DNAT 127.0.0.1:host → guestIP:guest, plus
//     POSTROUTING SNAT saddr 127.0.0.1 → HostIP on the TAP (guest cannot reply to
//     127.0.0.1). Guest egress uses MASQUERADE off the TAP.
//   - Live grain fwd add: host TCP proxy 127.0.0.1:host → guestIP:guest (no SSH).
//   - Overlay L2 and 9p/virtiofs mounts remain QEMU-only.
//
// Agent path stays on Firecracker vsock UDS + CONNECT (independent of TAP).

const (
	// FCNetMetaName is the sidecar under the VM dir describing TAP/IPs/DNAT.
	FCNetMetaName = "fc-net.json"
	// fcNetBaseOctet is the second octet of 10.77.0.0/16 (slot in third).
	fcNetBaseOctet = 77
	// fcNetSlotMin/Max are usable third-octet slots (avoid .0).
	fcNetSlotMin = 1
	fcNetSlotMax = 250
)

// FCNetPlan is the pure addressing plan for one Firecracker VM.
type FCNetPlan struct {
	Slot     int    `json:"slot"`
	TapName  string `json:"tap_name"`
	HostIP   string `json:"host_ip"`   // e.g. 10.77.3.1
	GuestIP  string `json:"guest_ip"`  // e.g. 10.77.3.2
	Prefix   int    `json:"prefix"`    // 24
	GuestMAC string `json:"guest_mac"` // AA:FC:…
	IfaceID  string `json:"iface_id"`  // eth0
}

// FCNetState is persisted next to the VM disk for cleanup and guest config.
type FCNetState struct {
	FCNetPlan
	// DNATRules are host-side publish mappings applied at Start (for teardown).
	DNATRules []FCDNATRule `json:"dnat_rules,omitempty"`
}

// FCDNATRule describes one DNAT publish (host loopback → guest).
type FCDNATRule struct {
	Proto     string `json:"proto"` // tcp|udp
	HostPort  int    `json:"host_port"`
	GuestPort int    `json:"guest_port"`
	GuestIP   string `json:"guest_ip"`
}

// PlanFCNet derives a deterministic plan from the VM name (pure, no syscalls).
func PlanFCNet(name string) FCNetPlan {
	slot := FCNetSlotForName(name)
	return FCNetPlan{
		Slot:     slot,
		TapName:  FCTapName(name),
		HostIP:   fmt.Sprintf("10.%d.%d.1", fcNetBaseOctet, slot),
		GuestIP:  fmt.Sprintf("10.%d.%d.2", fcNetBaseOctet, slot),
		Prefix:   24,
		GuestMAC: FCGuestMAC(name),
		IfaceID:  "eth0",
	}
}

// FCNetSlotForName maps a VM name to a third-octet slot in [1,250].
func FCNetSlotForName(name string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.TrimSpace(name)))
	// Map to 1..250 inclusive.
	return int(h.Sum32()%uint32(fcNetSlotMax-fcNetSlotMin+1)) + fcNetSlotMin
}

// FCTapName returns a short TAP device name (IFNAMSIZ-safe, max 15).
// Hash is non-cryptographic naming only (device name uniqueness).
func FCTapName(name string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(name)))
	// "tg" + 10 hex chars = 12 < 15.
	return "tg" + hex.EncodeToString(sum[:5])
}

// FCGuestMAC returns a locally-administered unicast MAC derived from name.
// Hash is non-cryptographic naming only (stable guest MAC).
func FCGuestMAC(name string) string {
	sum := sha256.Sum256([]byte("grain-fc-mac:" + strings.TrimSpace(name)))
	// Locally administered (0x02), unicast.
	return fmt.Sprintf("02:fc:%02x:%02x:%02x:%02x", sum[0], sum[1], sum[2], sum[3])
}

// BuildDNATRules builds loopback DNAT specs for SSH, optional agent TCP, and publishes.
// hostSSH/hostAgent are host ports (0 = skip). fwds are extra publishes.
func BuildDNATRules(guestIP string, hostSSH, hostAgent int, fwds []vm.PortForward) []FCDNATRule {
	var out []FCDNATRule
	if hostSSH > 0 {
		out = append(out, FCDNATRule{Proto: "tcp", HostPort: hostSSH, GuestPort: 22, GuestIP: guestIP})
	}
	if hostAgent > 0 {
		out = append(out, FCDNATRule{Proto: "tcp", HostPort: hostAgent, GuestPort: GuestAgentPort, GuestIP: guestIP})
	}
	for _, f := range fwds {
		if f.GuestPort <= 0 || f.HostPort <= 0 {
			continue
		}
		proto := f.Proto
		if proto == "" {
			proto = "tcp"
		}
		out = append(out, FCDNATRule{
			Proto:     proto,
			HostPort:  f.HostPort,
			GuestPort: f.GuestPort,
			GuestIP:   guestIP,
		})
	}
	return out
}

// GuestNetConfigScript returns a shell snippet to configure eth0 with static IP.
// Pure string helper for unit tests and agent exec.
func GuestNetConfigScript(plan FCNetPlan) string {
	// Use iproute2; ignore "File exists" on re-apply.
	return strings.TrimSpace(fmt.Sprintf(`
set -e
IFACE=%s
ip link set "$IFACE" up 2>/dev/null || true
# Replace any previous address on this iface for our subnet.
ip -4 addr flush dev "$IFACE" 2>/dev/null || true
ip addr add %s/%d dev "$IFACE" || true
ip route replace default via %s dev "$IFACE" || true
`, plan.IfaceID, plan.GuestIP, plan.Prefix, plan.HostIP))
}

// WriteFCNetState persists plan+rules under the VM directory.
func WriteFCNetState(vmDir string, st FCNetState) error {
	if vmDir == "" {
		return fmt.Errorf("empty vm dir")
	}
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(vmDir, FCNetMetaName), b, 0o644)
}

// ReadFCNetState loads fc-net.json if present.
func ReadFCNetState(vmDir string) (FCNetState, error) {
	var st FCNetState
	b, err := os.ReadFile(filepath.Join(vmDir, FCNetMetaName))
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

// RemoveFCNetState deletes the sidecar meta file.
func RemoveFCNetState(vmDir string) {
	if vmDir == "" {
		return
	}
	_ = os.Remove(filepath.Join(vmDir, FCNetMetaName))
}

// ParseHostPortPair validates a host listen address for the live TCP proxy.
func ParseHostPortPair(hostPort int) (string, error) {
	if hostPort <= 0 || hostPort > 65535 {
		return "", fmt.Errorf("invalid host port %d", hostPort)
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(hostPort)), nil
}

// HostToGuestSNATRule returns the match/target args for POSTROUTING SNAT used after
// OUTPUT DNAT of local clients. Without this, DNATed packets keep saddr=127.0.0.1
// and the guest cannot reply (replies target its own loopback). Pure helper.
//
// Full iptables form:
//
//	iptables -t nat -A POSTROUTING <HostToGuestSNATRule...>
func HostToGuestSNATRule(plan FCNetPlan) []string {
	return []string{
		"-o", plan.TapName,
		"-s", "127.0.0.1",
		"-j", "SNAT",
		"--to-source", plan.HostIP,
	}
}

// DNATRuleArgs returns match/target args for one create-time publish DNAT rule
// (chain OUTPUT or PREROUTING supplied by the caller). Pure helper.
//
//	iptables -t nat -A OUTPUT <DNATRuleArgs...>
func DNATRuleArgs(r FCDNATRule) []string {
	return []string{
		"-p", r.Proto,
		"-d", "127.0.0.1",
		"--dport", strconv.Itoa(r.HostPort),
		"-j", "DNAT",
		"--to-destination", net.JoinHostPort(r.GuestIP, strconv.Itoa(r.GuestPort)),
	}
}

// PrivilegeErrorHint is returned when TAP/NAT requires elevated privileges.
func PrivilegeErrorHint(op string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	if strings.Contains(low, "operation not permitted") ||
		strings.Contains(low, "permission denied") ||
		strings.Contains(low, "cap_net") {
		return fmt.Errorf("%s: %w (Firecracker networking needs CAP_NET_ADMIN — run grain as root or grant the daemon CAP_NET_ADMIN; ensure /dev/net/tun exists)", op, err)
	}
	return fmt.Errorf("%s: %w", op, err)
}

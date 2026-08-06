package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/vm"
	"github.com/spf13/cobra"
)

// parsePublishFlag parses a single --publish value.
// Accepted forms:
//
//	"8080:80"   → host 8080 → guest 80 (tcp)
//	"80"        → host auto (0) → guest 80 (tcp)
//	"udp/53:53" → host 53 → guest 53 (udp) — optional proto prefix on host side
//	"tcp/8080:80"
//
// Host ports below 1024 (except 0/auto) are rejected.
func parsePublishFlag(s string) (vm.PortForward, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return vm.PortForward{}, fmt.Errorf("empty --publish value")
	}

	proto := "tcp"
	if i := strings.Index(s, "/"); i > 0 {
		maybe := strings.ToLower(s[:i])
		if maybe == "tcp" || maybe == "udp" {
			proto = maybe
			s = s[i+1:]
		}
	}

	var hostPort, guestPort int
	var err error
	if strings.Contains(s, ":") {
		parts := strings.SplitN(s, ":", 2)
		if parts[0] == "" {
			hostPort = 0
		} else {
			hostPort, err = strconv.Atoi(parts[0])
			if err != nil {
				return vm.PortForward{}, fmt.Errorf("invalid host port in %q", s)
			}
		}
		guestPort, err = strconv.Atoi(parts[1])
		if err != nil {
			return vm.PortForward{}, fmt.Errorf("invalid guest port in %q", s)
		}
	} else {
		guestPort, err = strconv.Atoi(s)
		if err != nil {
			return vm.PortForward{}, fmt.Errorf("invalid port %q (want HOST:GUEST or GUEST)", s)
		}
		hostPort = 0
	}

	if guestPort <= 0 || guestPort > 65535 {
		return vm.PortForward{}, fmt.Errorf("guest port %d out of range", guestPort)
	}
	if hostPort < 0 || hostPort > 65535 {
		return vm.PortForward{}, fmt.Errorf("host port %d out of range", hostPort)
	}
	if hostPort > 0 && hostPort < 1024 {
		return vm.PortForward{}, fmt.Errorf("host port %d is privileged (< 1024); use 0/omit host for auto-allocate or pick a port >= 1024", hostPort)
	}

	return vm.PortForward{
		HostPort:  hostPort,
		GuestPort: guestPort,
		Proto:     proto,
	}, nil
}

func parsePublishFlags(vals []string) ([]vm.PortForward, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	out := make([]vm.PortForward, 0, len(vals))
	for _, v := range vals {
		f, err := parsePublishFlag(v)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func cmdFwd(cfgPath *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "fwd",
		Short: "Inspect and manage port forwards",
		Long: `Inspect host port mappings for grain VMs.

QEMU: create-time SLIRP hostfwd plus live SSH -L forwards.
Firecracker (vFC-2): create-time DNAT publish on TAP + live TCP proxy
(grain fwd add) to the guest IP (no SSH required). Agent path remains vsock.

Create-time SLIRP forwards:

  grain new --publish 8080:80 -P 4430:443

Live SSH local forwards (running VM only):

  grain fwd add [name] 8080:80
  grain fwd add [name] 80
  grain fwd rm  [name] 8080
  grain fwd ls  [name]

Live forwards use managed "ssh -N -L" tunnels via the grain identity.
They are cleared on stop/delete and are not re-applied on start.

Remote laptop (daemon host loopback):

  Published and live host ports bind 127.0.0.1 on the grain host.
  Print ready-to-run SSH local forwards for those ports:

  grain fwd tunnel [name]
  grain fwd tunnel [name] --host sandbox.example.com --user alice
  grain fwd tunnel web --json

  GRAIN_SSH_HOST supplies the default --host. See the remote-lab guide.`,
	}
	c.AddCommand(cmdFwdLs(cfgPath))
	c.AddCommand(cmdFwdAdd(cfgPath))
	c.AddCommand(cmdFwdRm(cfgPath))
	c.AddCommand(cmdFwdTunnel(cfgPath))
	return c
}

// tunnelPort is one host-side published port (SLIRP hostfwd or live SSH -L).
type tunnelPort struct {
	HostPort  int    `json:"host_port"`
	GuestPort int    `json:"guest_port"`
	Kind      string `json:"kind"` // slirp | live
}

// tunnelLine is a ready-to-run SSH local-forward command for agents/humans.
type tunnelLine struct {
	Name      string `json:"name"`
	HostPort  int    `json:"host_port"`
	GuestPort int    `json:"guest_port"`
	Kind      string `json:"kind"`
	SSH       string `json:"ssh"`
}

// sshTunnelTarget builds the SSH destination for tunnel lines.
// Empty host falls back to GRAIN_SSH_HOST; if still empty, uses USER@HOST
// (or user@HOST when --user is set) as a copy-paste template.
func sshTunnelTarget(user, host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = strings.TrimSpace(os.Getenv("GRAIN_SSH_HOST"))
	}
	user = strings.TrimSpace(user)
	if host == "" {
		if user == "" {
			return "USER@HOST"
		}
		return user + "@HOST"
	}
	if user == "" {
		return host
	}
	return user + "@" + host
}

// formatSSHTunnelLine returns a single ready-to-run local forward:
//
//	ssh -N -L HOSTPORT:127.0.0.1:HOSTPORT target
//
// Hostfwd and live forwards bind on the daemon host's 127.0.0.1; the laptop
// mirrors the same host port locally for a simple browser URL.
func formatSSHTunnelLine(hostPort int, target string) string {
	return fmt.Sprintf("ssh -N -L %d:127.0.0.1:%d %s", hostPort, hostPort, target)
}

// collectHostForwardPorts returns unique host ports from SLIRP Forwards and
// LiveForwards (HostPort > 0 only). SLIRP entries come first; duplicate host
// ports from live forwards are skipped.
func collectHostForwardPorts(inst *vm.Instance) []tunnelPort {
	if inst == nil {
		return nil
	}
	seen := make(map[int]struct{})
	var out []tunnelPort
	for _, f := range inst.Forwards {
		if f.HostPort <= 0 {
			continue
		}
		if _, ok := seen[f.HostPort]; ok {
			continue
		}
		seen[f.HostPort] = struct{}{}
		proto := f.Proto
		if proto == "" {
			proto = "tcp"
		}
		// Only TCP is useful for ssh -L; skip explicit non-tcp.
		if proto != "tcp" {
			continue
		}
		out = append(out, tunnelPort{
			HostPort:  f.HostPort,
			GuestPort: f.GuestPort,
			Kind:      "slirp",
		})
	}
	for _, f := range inst.LiveForwards {
		if f.HostPort <= 0 {
			continue
		}
		if _, ok := seen[f.HostPort]; ok {
			continue
		}
		seen[f.HostPort] = struct{}{}
		out = append(out, tunnelPort{
			HostPort:  f.HostPort,
			GuestPort: f.GuestPort,
			Kind:      "live",
		})
	}
	return out
}

// buildTunnelLines formats SSH -L commands for each host forward on inst.
func buildTunnelLines(inst *vm.Instance, target string) []tunnelLine {
	ports := collectHostForwardPorts(inst)
	if len(ports) == 0 {
		return nil
	}
	out := make([]tunnelLine, 0, len(ports))
	for _, p := range ports {
		out = append(out, tunnelLine{
			Name:      inst.Name,
			HostPort:  p.HostPort,
			GuestPort: p.GuestPort,
			Kind:      p.Kind,
			SSH:       formatSSHTunnelLine(p.HostPort, target),
		})
	}
	return out
}

func cmdFwdTunnel(cfgPath *string) *cobra.Command {
	var (
		userFlag string
		hostFlag string
		asJSON   bool
	)
	c := &cobra.Command{
		Use:   "tunnel [name]",
		Short: "Print SSH -L lines to reach hostfwd ports from a laptop",
		Long: `Print ready-to-run ssh -N -L commands for a VM's published and live host ports.

Hostfwd and live forwards bind on the daemon host's 127.0.0.1. From a remote
laptop, tunnel those ports back to your machine (same pattern as the remote-lab
guide):

  grain fwd tunnel web --host sandbox.example.com
  # ssh -N -L 3000:127.0.0.1:3000 sandbox.example.com

  grain fwd tunnel --user alice --host sandbox.example.com
  export GRAIN_SSH_HOST=sandbox.example.com
  grain fwd tunnel web

With no --host and no GRAIN_SSH_HOST, lines use a USER@HOST placeholder.
--json emits machine-readable entries (name, host_port, guest_port, kind, ssh).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}

			var list []*vm.Instance
			if len(args) == 1 && args[0] != "" {
				inst, err := c.Get(ctx, args[0])
				if err != nil {
					return err
				}
				list = []*vm.Instance{inst}
			} else {
				list, err = c.List(ctx)
				if err != nil {
					return err
				}
			}
			if len(list) == 0 {
				return fmt.Errorf("no vms — create one:  grain new -P 8080:80")
			}

			target := sshTunnelTarget(userFlag, hostFlag)
			var lines []tunnelLine
			for _, inst := range list {
				lines = append(lines, buildTunnelLines(inst, target)...)
			}
			if len(lines) == 0 {
				if asJSON {
					fmt.Println("[]")
					return nil
				}
				fmt.Println("no host ports to tunnel — publish with -P HOST:GUEST or grain fwd add")
				return nil
			}

			if asJSON {
				b, err := json.MarshalIndent(lines, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(b))
				return nil
			}

			for _, ln := range lines {
				fmt.Println(ln.SSH)
			}
			return nil
		},
	}
	c.Flags().StringVar(&userFlag, "user", "", "SSH user on the daemon host (optional)")
	c.Flags().StringVar(&hostFlag, "host", "", "SSH host of the daemon machine (default: GRAIN_SSH_HOST or USER@HOST placeholder)")
	c.Flags().BoolVar(&asJSON, "json", false, "machine-readable tunnel list")
	return c
}

func cmdFwdLs(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ls [name]",
		Short: "List port forwards for VM(s)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}

			var list []*vm.Instance
			if len(args) == 1 && args[0] != "" {
				inst, err := c.Get(ctx, args[0])
				if err != nil {
					return err
				}
				list = []*vm.Instance{inst}
			} else {
				list, err = c.List(ctx)
				if err != nil {
					return err
				}
			}
			if len(list) == 0 {
				fmt.Println("no vms — create one:  grain new -P 8080:80")
				return nil
			}

			fmt.Printf("%-12s %-6s %-10s %-10s %s\n", "NAME", "PROTO", "HOST", "GUEST", "NOTE")
			any := false
			for _, inst := range list {
				if inst.SSHPort > 0 {
					fmt.Printf("%-12s %-6s %-10s %-10s %s\n",
						inst.Name, "tcp", fmt.Sprintf(":%d", inst.SSHPort), "22", "ssh")
					any = true
				}
				for _, f := range inst.Forwards {
					proto := f.Proto
					if proto == "" {
						proto = "tcp"
					}
					host := "-"
					if f.HostPort > 0 {
						host = fmt.Sprintf(":%d", f.HostPort)
					}
					fmt.Printf("%-12s %-6s %-10s %-10d %s\n",
						inst.Name, proto, host, f.GuestPort, "slirp")
					any = true
				}
				for _, f := range inst.LiveForwards {
					host := fmt.Sprintf(":%d", f.HostPort)
					note := "live"
					if f.PID > 0 {
						note = fmt.Sprintf("live pid=%d", f.PID)
					}
					fmt.Printf("%-12s %-6s %-10s %-10d %s\n",
						inst.Name, "tcp", host, f.GuestPort, note)
					any = true
				}
				if inst.SSHPort == 0 && len(inst.Forwards) == 0 && len(inst.LiveForwards) == 0 {
					fmt.Printf("%-12s %-6s %-10s %-10s %s\n",
						inst.Name, "-", "-", "-", "(none)")
				}
			}
			_ = any
			return nil
		},
	}
}

func cmdFwdAdd(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "add [name] HOST:GUEST",
		Short: "Add a live SSH local port forward on a running VM",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}

			var name, spec string
			if len(args) == 1 {
				spec = args[0]
				name, err = resolveVMName(c, nil, false)
				if err != nil {
					return err
				}
			} else {
				name = args[0]
				spec = args[1]
			}

			fwd, err := parsePublishFlag(spec)
			if err != nil {
				return err
			}
			lf, err := c.AddForward(ctx, name, fwd.HostPort, fwd.GuestPort)
			if err != nil {
				return err
			}
			fmt.Printf("forward %s  host=:%d → guest=%d  pid=%d\n", name, lf.HostPort, lf.GuestPort, lf.PID)
			return nil
		},
	}
}

func cmdFwdRm(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rm [name] HOST_PORT",
		Short: "Remove a live SSH local port forward",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}

			var name string
			var hostPort int
			if len(args) == 1 {
				hostPort, err = strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("host port: %w", err)
				}
				name, err = resolveVMName(c, nil, false)
				if err != nil {
					return err
				}
			} else {
				name = args[0]
				hostPort, err = strconv.Atoi(args[1])
				if err != nil {
					return fmt.Errorf("host port: %w", err)
				}
			}
			if hostPort <= 0 {
				return fmt.Errorf("host port must be positive")
			}
			if err := c.RemoveForward(ctx, name, hostPort); err != nil {
				return err
			}
			fmt.Printf("removed forward %s host=:%d\n", name, hostPort)
			return nil
		},
	}
}

package cli

import (
	"context"
	"fmt"
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
	// optional proto/ prefix: tcp/8080:80 or udp/53:53
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
			// ":80" same as "80" (auto host)
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

// parsePublishFlags parses all --publish / -P values.
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
		Short: "Inspect port forwards",
		Long: `Inspect SLIRP hostfwd port mappings for grain VMs.

Port forwards are set at create time with:

  grain new --publish 8080:80 -P 4430:443

Host ports with value 0 (or omitted: -P 80) are allocated free ports and
persisted. Forwards apply on create and again on start (from stored meta).

Note: changing forwards after create is not supported yet — recreate the
VM or re-create with the desired --publish flags. A restart re-applies
the stored forwards but does not hot-add new ones.`,
	}
	c.AddCommand(cmdFwdLs(cfgPath))
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
			c := clientFrom(cfg)
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
				// Always show SSH as the built-in forward
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
						inst.Name, proto, host, f.GuestPort, "")
					any = true
				}
				if inst.SSHPort == 0 && len(inst.Forwards) == 0 {
					fmt.Printf("%-12s %-6s %-10s %-10s %s\n",
						inst.Name, "-", "-", "-", "(none)")
				}
			}
			if !any {
				return nil
			}
			return nil
		},
	}
}

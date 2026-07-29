package cli

import (
	"fmt"
	"strings"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vm"
	"github.com/spf13/cobra"
)

func cmdProfile(cfgPath *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "profile",
		Short: "Manage named create profiles",
	}
	c.AddCommand(cmdProfileLs(cfgPath))
	return c
}

func cmdProfileLs(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List named profiles from config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			names := cfg.ProfileNames()
			if len(names) == 0 {
				fmt.Println("no profiles — add profiles: under ~/.grain/config.yaml")
				return nil
			}
			fmt.Printf("%-16s %-5s %-8s %-6s %-14s %-8s %s\n",
				"NAME", "CPUS", "MEM", "DISK", "IMAGE", "PERSIST", "PRESET/MOUNTS/FWDS")
			for _, name := range names {
				p, err := cfg.LookupProfile(name)
				if err != nil {
					return err
				}
				cpus := "-"
				if p.CPUs > 0 {
					cpus = fmt.Sprintf("%d", p.CPUs)
				}
				mem := "-"
				if p.MemoryMB > 0 {
					mem = fmt.Sprintf("%d", p.MemoryMB)
				}
				disk := "-"
				if p.DiskGB > 0 {
					disk = fmt.Sprintf("%d", p.DiskGB)
				}
				img := p.Image
				if img == "" {
					img = "-"
				}
				preset := p.Preset
				if preset == "" {
					preset = "-"
				}
				src := "config"
				if cfg.Profiles == nil {
					src = "builtin"
				} else if _, ok := cfg.Profiles[name]; !ok {
					src = "builtin"
				}
				extra := fmt.Sprintf("%s m=%d f=%d (%s)", preset, len(p.Mounts), len(p.Forwards), src)
				fmt.Printf("%-16s %-5s %-8s %-6s %-14s %-8v %s\n",
					name, cpus, mem, disk, img, p.Persistent, extra)
			}
			return nil
		},
	}
}

// profileMountsToVM converts profile mounts to API mount specs.
func profileMountsToVM(in []config.ProfileMount) []vm.Mount {
	if len(in) == 0 {
		return nil
	}
	out := make([]vm.Mount, 0, len(in))
	for _, m := range in {
		out = append(out, vm.Mount{Host: m.Host, Guest: m.Guest})
	}
	return out
}

// profileForwardsToVM converts profile forwards to API port forwards.
func profileForwardsToVM(in []config.ProfileForward) []vm.PortForward {
	if len(in) == 0 {
		return nil
	}
	out := make([]vm.PortForward, 0, len(in))
	for _, f := range in {
		proto := f.Proto
		if proto == "" {
			proto = "tcp"
		}
		out = append(out, vm.PortForward{
			HostPort:  f.HostPort,
			GuestPort: f.GuestPort,
			Proto:     proto,
		})
	}
	return out
}

// profileSummary is a one-line human summary (tests / debug).
func profileSummary(name string, p config.Profile) string {
	var parts []string
	if p.CPUs > 0 {
		parts = append(parts, fmt.Sprintf("cpus=%d", p.CPUs))
	}
	if p.MemoryMB > 0 {
		parts = append(parts, fmt.Sprintf("mem=%d", p.MemoryMB))
	}
	if p.DiskGB > 0 {
		parts = append(parts, fmt.Sprintf("disk=%d", p.DiskGB))
	}
	if p.Image != "" {
		parts = append(parts, "image="+p.Image)
	}
	if p.Preset != "" {
		parts = append(parts, "preset="+p.Preset)
	}
	if len(p.Mounts) > 0 {
		parts = append(parts, fmt.Sprintf("mounts=%d", len(p.Mounts)))
	}
	if len(p.Forwards) > 0 {
		parts = append(parts, fmt.Sprintf("fwds=%d", len(p.Forwards)))
	}
	if len(parts) == 0 {
		return name
	}
	return name + " (" + strings.Join(parts, " ") + ")"
}

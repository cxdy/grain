package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/spf13/cobra"
)

func cmdFs(cfgPath *string) *cobra.Command {
	root := &cobra.Command{
		Use:   "fs",
		Short: "Guest filesystem helpers via grain-agent (no SSH)",
	}
	root.AddCommand(
		cmdFsLs(cfgPath),
		cmdFsStat(cfgPath),
		cmdFsMkdir(cfgPath),
		cmdFsRm(cfgPath),
	)
	return root
}

// resolveVMAndPath resolves optional VM name + required guest path.
// One arg: path only (single VM). Two args: name PATH.
func resolveVMAndPath(c *api.Client, args []string) (name, path string, err error) {
	switch len(args) {
	case 1:
		name, err = resolveVMName(c, nil, false)
		if err != nil {
			return "", "", err
		}
		return name, args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", fmt.Errorf("need PATH or NAME PATH")
	}
}

func cmdFsLs(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ls [name] PATH",
		Short: "List guest directory (agent)",
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
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var entries []struct {
				Name string
				Type string
				Mode string
				Size int64
			}
			// Prefer daemon-proxied API when remote (or always for simplicity).
			if remoteMode(cfg) {
				list, err := c.ReadDir(ctx, name, path)
				if err != nil {
					return err
				}
				for _, e := range list {
					entries = append(entries, struct {
						Name string
						Type string
						Mode string
						Size int64
					}{e.Name, e.Type, e.Mode, e.Size})
				}
			} else {
				ac, err := dialGuestAgent(c, name, true)
				if err != nil {
					return err
				}
				list, err := ac.ReadDir(ctx, path)
				if err != nil {
					return err
				}
				for _, e := range list {
					entries = append(entries, struct {
						Name string
						Type string
						Mode string
						Size int64
					}{e.Name, e.Type, e.Mode, e.Size})
				}
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, e := range entries {
				kind := e.Type
				if kind == "" {
					kind = "?"
				}
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", kind, e.Mode, e.Size, e.Name)
			}
			return tw.Flush()
		},
	}
}

func cmdFsStat(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stat [name] PATH",
		Short: "Stat guest path (agent)",
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
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var nameOut, typeOut, modeOut string
			var sizeOut, mtimeOut int64
			if remoteMode(cfg) {
				info, err := c.Stat(ctx, name, path)
				if err != nil {
					return err
				}
				nameOut, typeOut, modeOut, sizeOut, mtimeOut = info.Name, info.Type, info.Mode, info.Size, info.Mtime
			} else {
				ac, err := dialGuestAgent(c, name, true)
				if err != nil {
					return err
				}
				info, err := ac.Stat(ctx, path)
				if err != nil {
					return err
				}
				nameOut, typeOut, modeOut, sizeOut, mtimeOut = info.Name, info.Type, info.Mode, info.Size, info.Mtime
			}
			fmt.Printf("name:  %s\n", nameOut)
			fmt.Printf("type:  %s\n", typeOut)
			fmt.Printf("size:  %d\n", sizeOut)
			fmt.Printf("mode:  %s\n", modeOut)
			if mtimeOut > 0 {
				fmt.Printf("mtime: %s\n", time.Unix(mtimeOut, 0).UTC().Format(time.RFC3339))
			}
			return nil
		},
	}
}

func cmdFsMkdir(cfgPath *string) *cobra.Command {
	var parents bool
	cmd := &cobra.Command{
		Use:   "mkdir [name] PATH",
		Short: "Create guest directory (agent)",
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
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if remoteMode(cfg) {
				return c.Mkdir(ctx, name, path, parents, "")
			}
			ac, err := dialGuestAgent(c, name, true)
			if err != nil {
				return err
			}
			return ac.Mkdir(ctx, path, parents, "")
		},
	}
	cmd.Flags().BoolVarP(&parents, "parents", "p", false, "create parent directories as needed")
	return cmd
}

func cmdFsRm(cfgPath *string) *cobra.Command {
	var recursive bool
	cmd := &cobra.Command{
		Use:   "rm [name] PATH",
		Short: "Remove guest path (agent)",
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
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if remoteMode(cfg) {
				return c.Remove(ctx, name, path, recursive)
			}
			ac, err := dialGuestAgent(c, name, true)
			if err != nil {
				return err
			}
			return ac.Remove(ctx, path, recursive)
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "remove directories recursively")
	return cmd
}

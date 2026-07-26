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
			c := clientFrom(cfg)
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ac, err := dialGuestAgent(c, name, true)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			entries, err := ac.ReadDir(ctx, path)
			if err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			for _, e := range entries {
				kind := e.Type
				if kind == "" {
					kind = "?"
				}
				fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", kind, e.Mode, e.Size, e.Name)
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
			c := clientFrom(cfg)
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ac, err := dialGuestAgent(c, name, true)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			info, err := ac.Stat(ctx, path)
			if err != nil {
				return err
			}
			fmt.Printf("name:  %s\n", info.Name)
			fmt.Printf("type:  %s\n", info.Type)
			fmt.Printf("size:  %d\n", info.Size)
			fmt.Printf("mode:  %s\n", info.Mode)
			if info.Mtime > 0 {
				fmt.Printf("mtime: %s\n", time.Unix(info.Mtime, 0).UTC().Format(time.RFC3339))
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
			c := clientFrom(cfg)
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ac, err := dialGuestAgent(c, name, true)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
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
			c := clientFrom(cfg)
			name, path, err := resolveVMAndPath(c, args)
			if err != nil {
				return err
			}
			ac, err := dialGuestAgent(c, name, true)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return ac.Remove(ctx, path, recursive)
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "remove directories recursively")
	return cmd
}

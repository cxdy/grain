package cli

import (
	"fmt"
	"strings"

	"github.com/cxdy/grain/internal/recipe"
	"github.com/spf13/cobra"
)

func cmdRecipe() *cobra.Command {
	c := &cobra.Command{
		Use:   "recipe",
		Short: "Validate and inspect sandbox recipe files",
		Long: `Sandbox recipes are portable YAML files that describe create options and
optional bootstrap steps (readiness protocol). Apply with:

  grain new --recipe ./lab.recipe.yaml

Commands here validate and show the compiled userdata without creating a VM.`,
	}
	c.AddCommand(cmdRecipeValidate())
	c.AddCommand(cmdRecipeShow())
	return c
}

func cmdRecipeValidate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate a sandbox recipe YAML file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := recipe.Load(args[0])
			if err != nil {
				return err
			}
			if _, err := f.Compile(); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "ok  %s  apiVersion=%s kind=%s", f.SourcePath, f.APIVersion, f.Kind)
			if n := strings.TrimSpace(f.Metadata.Name); n != "" {
				fmt.Fprintf(w, " name=%s", n)
			}
			if n := len(f.Spec.Bootstrap.Steps); n > 0 {
				fmt.Fprintf(w, " bootstrap_steps=%d", n)
			}
			fmt.Fprintln(w)
			return nil
		},
	}
}

func cmdRecipeShow() *cobra.Command {
	var showUserdata bool
	cmd := &cobra.Command{
		Use:   "show <file>",
		Short: "Show compiled create options (and optional userdata) for a recipe",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := recipe.Load(args[0])
			if err != nil {
				return err
			}
			c, err := f.Compile()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "name:           %s\n", emptyDash(c.Name))
			fmt.Fprintf(w, "description:    %s\n", emptyDash(c.Description))
			fmt.Fprintf(w, "image:          %s\n", emptyDash(c.Image))
			fmt.Fprintf(w, "cpus:           %d\n", c.CPUs)
			fmt.Fprintf(w, "memory_mb:      %d\n", c.MemoryMB)
			fmt.Fprintf(w, "disk_gb:        %d\n", c.DiskGB)
			fmt.Fprintf(w, "persistent:     %v\n", c.Persistent)
			fmt.Fprintf(w, "arch:           %s\n", emptyDash(c.Arch))
			fmt.Fprintf(w, "gpu:            %s\n", emptyDash(c.GPU))
			fmt.Fprintf(w, "network:        %s\n", emptyDash(c.Network))
			fmt.Fprintf(w, "preset:         %s\n", emptyDash(c.Preset))
			fmt.Fprintf(w, "wait:           %s\n", emptyDash(c.Wait))
			fmt.Fprintf(w, "ready_timeout:  %s\n", emptyDash(c.Timeout))
			fmt.Fprintf(w, "bootstrap:      %v\n", c.HasBootstrap)
			fmt.Fprintf(w, "ready_name:     %s\n", emptyDash(c.ReadyName))
			fmt.Fprintf(w, "mounts:         %d\n", len(c.Mounts))
			for _, m := range c.Mounts {
				fmt.Fprintf(w, "  %s → %s\n", m.Host, m.Guest)
			}
			fmt.Fprintf(w, "forwards:       %d\n", len(c.Forwards))
			for _, fwd := range c.Forwards {
				fmt.Fprintf(w, "  :%d → %d (%s)\n", fwd.HostPort, fwd.GuestPort, fwd.Proto)
			}
			fmt.Fprintf(w, "socket_fwds:    %d\n", len(c.SocketForwards))
			if showUserdata {
				fmt.Fprintln(w, "--- userdata ---")
				fmt.Fprint(w, c.Userdata)
				if c.Userdata != "" && !strings.HasSuffix(c.Userdata, "\n") {
					fmt.Fprintln(w)
				}
			} else if strings.TrimSpace(c.Userdata) != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "tip: pass --userdata to print compiled cloud-init")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showUserdata, "userdata", false, "print compiled cloud-init / bootstrap script")
	return cmd
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

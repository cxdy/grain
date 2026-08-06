package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/cxdy/grain/internal/recipe"
	"github.com/spf13/cobra"
)

func cmdRecipe() *cobra.Command {
	c := &cobra.Command{
		Use:   "recipe",
		Short: "Manage sandbox recipe library and files",
		Long: `Sandbox recipes are portable YAML files (apiVersion: grain/v1, kind: Sandbox)
that describe create options and optional bootstrap steps.

Library (default ~/.grain/recipes/<name>.yaml):
  grain recipe list
  grain recipe add ./lab.yaml
  grain recipe add https://example.com/lab.yaml
  grain recipe add git-lab          # from official catalog (after search)
  grain recipe search               # browse official index (no install)
  grain recipe show git-lab
  grain recipe validate git-lab
  grain recipe delete git-lab

Preview without installing:
  grain recipe preview ./lab.yaml
  grain recipe preview https://example.com/lab.yaml

Create from a library name or path:
  grain new --recipe git-lab
  grain new --recipe ./lab.yaml

Official recipes live in the grain repo (recipes/) and are added via PR only.
`,
	}
	c.AddCommand(cmdRecipeList())
	c.AddCommand(cmdRecipeAdd())
	c.AddCommand(cmdRecipeSearch())
	c.AddCommand(cmdRecipePreview())
	c.AddCommand(cmdRecipeValidate())
	c.AddCommand(cmdRecipeShow())
	c.AddCommand(cmdRecipeDelete())
	return c
}

func recipeLibraryDir() string {
	return recipe.DefaultLibraryDir()
}

func loadRecipeArg(nameOrPath string) (*recipe.File, error) {
	return recipe.LoadResolved(recipeLibraryDir(), nameOrPath)
}

func cmdRecipeList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List recipes in the local library (~/.grain/recipes)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := recipe.ListLibrary(recipeLibraryDir())
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(list) == 0 {
				fmt.Fprintln(w, "No recipes in library. Import with: grain recipe add <file|url|catalog-id>")
				return nil
			}
			fmt.Fprintf(w, "%-20s %-16s %-8s %s\n", "ID", "IMAGE", "BOOT", "DESCRIPTION")
			for _, e := range list {
				boot := "no"
				if e.HasBootstrap {
					boot = "yes"
				}
				desc := e.Description
				if desc == "" {
					desc = e.Name
				}
				fmt.Fprintf(w, "%-20s %-16s %-8s %s\n", e.ID, emptyDash(e.Image), boot, desc)
			}
			return nil
		},
	}
}

func cmdRecipeAdd() *cobra.Command {
	var overwrite bool
	var id string
	cmd := &cobra.Command{
		Use:   "add <file|url|catalog-id>",
		Short: "Add a recipe to the local library (never creates a VM)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := strings.TrimSpace(args[0])
			opts := recipe.SaveOptions{Overwrite: overwrite, ID: id}
			lib := recipeLibraryDir()
			var ent recipe.LibraryEntry
			var err error
			switch {
			case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
				if strings.HasPrefix(src, "http://") {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning: using cleartext HTTP for recipe download")
				}
				ent, err = recipe.AddFromURL(nil, lib, src, "", opts)
			case fileExists(src) || strings.Contains(src, string(os.PathSeparator)) || strings.HasPrefix(src, "./") || strings.HasPrefix(src, "../"):
				ent, err = recipe.AddFile(lib, src, opts)
			default:
				// catalog id
				cat, cerr := recipe.FetchCatalog(nil, recipe.CatalogURL(), recipe.CatalogCachePath())
				if cerr != nil {
					return fmt.Errorf("catalog: %w (or pass a file path / URL)", cerr)
				}
				ent, err = recipe.AddFromCatalog(nil, cat, lib, src, opts)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "added  %s  →  %s\n", ent.ID, ent.Path)
			fmt.Fprintln(cmd.OutOrStdout(), "Deploy with: grain new --recipe", ent.ID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace an existing library recipe with the same id")
	cmd.Flags().StringVar(&id, "id", "", "library id (filename stem); default from metadata.name or source name")
	return cmd
}

func cmdRecipeSearch() *cobra.Command {
	return &cobra.Command{
		Use:   "search",
		Short: "List official catalog recipes (index only; does not download bodies)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cat, err := recipe.FetchCatalog(nil, recipe.CatalogURL(), recipe.CatalogCachePath())
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(cat.Recipes) == 0 {
				fmt.Fprintln(w, "Catalog is empty.")
				return nil
			}
			libIDs := map[string]bool{}
			if list, err := recipe.ListLibrary(recipeLibraryDir()); err == nil {
				for _, e := range list {
					libIDs[e.ID] = true
				}
			}
			fmt.Fprintf(w, "%-20s %-8s %s\n", "ID", "LOCAL", "TITLE")
			for _, e := range cat.Recipes {
				local := "no"
				if libIDs[e.ID] {
					local = "yes"
				}
				title := e.Title
				if title == "" {
					title = e.Description
				}
				fmt.Fprintf(w, "%-20s %-8s %s\n", e.ID, local, title)
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "Install one: grain recipe add <id>")
			return nil
		},
	}
}

func cmdRecipeDelete() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Remove a recipe from the local library (does not delete sandboxes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := recipe.DeleteLibrary(recipeLibraryDir(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "deleted  %s\n", args[0])
			return nil
		},
	}
}

func cmdRecipePreview() *cobra.Command {
	return &cobra.Command{
		Use:   "preview <file|url|name>",
		Short: "Validate and summarize a recipe without installing or creating a VM",
		Long: `Fetch (if URL) or load a recipe and print a human summary: image, resources,
mounts, forwards, bootstrap steps, and trust warnings. Does not write the library.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src := strings.TrimSpace(args[0])
			w := cmd.OutOrStdout()
			var prev recipe.RecipePreview
			var err error
			switch {
			case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
				if strings.HasPrefix(src, "http://") {
					fmt.Fprintln(cmd.ErrOrStderr(), "warning: cleartext HTTP")
				}
				prev, err = recipe.PreviewFromURL(nil, src, "")
			default:
				// file or library name
				var f *recipe.File
				if fileExists(src) || strings.Contains(src, string(os.PathSeparator)) || strings.HasPrefix(src, "./") {
					f, err = recipe.Load(src)
				} else {
					f, err = loadRecipeArg(src)
				}
				if err != nil {
					return err
				}
				b, rerr := os.ReadFile(f.SourcePath)
				if rerr != nil {
					// Parse already validated; re-marshal for preview body
					b, err = f.MarshalYAML()
					if err != nil {
						return err
					}
				}
				prev, err = recipe.PreviewFromYAML(b)
				if err != nil {
					return err
				}
				if prev.SuggestedID == "" {
					prev.SuggestedID = f.Metadata.Name
				}
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "id:             %s\n", emptyDash(prev.SuggestedID))
			fmt.Fprintf(w, "name:           %s\n", emptyDash(prev.Name))
			fmt.Fprintf(w, "description:    %s\n", emptyDash(prev.Description))
			fmt.Fprintf(w, "image:          %s\n", emptyDash(prev.Image))
			fmt.Fprintf(w, "cpus:           %d\n", prev.CPUs)
			fmt.Fprintf(w, "memory_mb:      %d\n", prev.MemoryMB)
			fmt.Fprintf(w, "disk_gb:        %d\n", prev.DiskGB)
			fmt.Fprintf(w, "persistent:     %v\n", prev.Persistent)
			fmt.Fprintf(w, "bootstrap:      %v\n", prev.HasBootstrap)
			if len(prev.BootstrapSteps) > 0 {
				fmt.Fprintf(w, "steps:          %s\n", strings.Join(prev.BootstrapSteps, ", "))
			}
			fmt.Fprintf(w, "mounts:         %d\n", len(prev.Mounts))
			for _, m := range prev.Mounts {
				fmt.Fprintf(w, "  %s\n", m)
			}
			fmt.Fprintf(w, "forwards:       %d\n", len(prev.Forwards))
			for _, f := range prev.Forwards {
				fmt.Fprintf(w, "  %s\n", f)
			}
			if len(prev.Warnings) > 0 {
				fmt.Fprintln(w, "warnings:")
				for _, warn := range prev.Warnings {
					fmt.Fprintf(w, "  - %s\n", warn)
				}
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "tip: grain recipe add <src>  # install to library (does not create a VM)")
			return nil
		},
	}
}

func cmdRecipeValidate() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <name|file>",
		Short: "Validate a sandbox recipe (library name or file path)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := loadRecipeArg(args[0])
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
		Use:   "show <name|file>",
		Short: "Show compiled create options for a library recipe or file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := loadRecipeArg(args[0])
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

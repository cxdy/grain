package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cxdy/grain/internal/config"
	"github.com/spf13/cobra"
)

// cmdCheckConfig validates a grain config file (parse + field checks).
func cmdCheckConfig(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "check-config [file]",
		Short: "Validate a grain config file",
		Long: `Parse and validate a grain configuration file.

  grain check-config
  grain check-config /path/to/config.yaml
  grain check-config --config /path/to/config.yaml

Exit 0 when the file is valid; non-zero with errors on stderr.
Used by Grain Desktop before applying config edits.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 && args[0] != "" {
				path = args[0]
			} else if cfgPath != nil && *cfgPath != "" {
				path = *cfgPath
			}
			if path == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				path = filepath.Join(home, ".grain", "config.yaml")
			}
			cfg, err := config.ValidateFile(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ok: %s (hypervisor=%s image=%s)\n", path, cfg.Hypervisor, cfg.Image)
			return nil
		},
	}
}

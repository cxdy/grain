package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func cmdStats(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stats [name]",
		Short: "Guest resource stats via grain-agent (cpu/mem/disk basics)",
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
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			st, err := c.Stats(ctx, name)
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(st, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	}
}

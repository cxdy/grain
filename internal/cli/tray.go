package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/cxdy/grain/internal/tray"
	"github.com/spf13/cobra"
)

func cmdTray(cfgPath *string, version string) *cobra.Command {
	return &cobra.Command{
		Use:   "tray",
		Short: "Menu bar / system tray control for the local daemon",
		Long: `Run a small menu bar (macOS) or system tray (Linux) helper.

Shows daemon status and a live VM count, with actions to open the docs
and stop the tray. The grain daemon is separate — run grain up first.

  grain up
  grain tray
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS == "windows" {
				return fmt.Errorf("grain tray is not supported on Windows")
			}
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			if err := requireLocalDaemon(cfg, "grain tray"); err != nil {
				// Soft: tray can still start and show "daemon down"
				fmt.Fprintf(os.Stderr, "note: %v\n", err)
			}
			return tray.Run(tray.Options{
				Version: version,
				Status: func(ctx context.Context) tray.Status {
					return trayStatus(cfgPath)
				},
				OnQuit: func() {},
			})
		},
	}
}

func trayStatus(cfgPath *string) tray.Status {
	st := tray.Status{Tooltip: "grain"}
	cfg, err := loadCfg(cfgPath)
	if err != nil {
		st.Title = "grain ?"
		st.Tooltip = err.Error()
		return st
	}
	c, err := clientFrom(cfg)
	if err != nil {
		st.Title = "grain · off"
		st.Tooltip = "daemon not reachable — grain up"
		return st
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		st.Title = "grain · off"
		st.Tooltip = "daemon not reachable — grain up"
		return st
	}
	list, err := c.List(ctx)
	if err != nil {
		st.Title = "grain · on"
		st.Tooltip = "daemon up"
		return st
	}
	n := 0
	for _, inst := range list {
		if inst != nil && (inst.Status == "running" || inst.Status == "creating" || inst.Status == "paused") {
			n++
		}
	}
	st.Title = fmt.Sprintf("grain · %d", n)
	st.Tooltip = fmt.Sprintf("daemon up · %d active VM(s)", n)
	return st
}

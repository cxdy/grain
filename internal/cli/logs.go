package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/spf13/cobra"
)

// cmdLogs prints or follows per-VM serial / QEMU hypervisor logs.
func cmdLogs(cfgPath *string) *cobra.Command {
	var follow bool
	var useQEMU bool
	cmd := &cobra.Command{
		Use:   "logs [name]",
		Short: "Print or follow guest serial / QEMU logs",
		Long: `Print guest serial console output (default) or the QEMU process log.

  grain logs              # only VM, or list if many
  grain logs sbox-1
  grain logs -f sbox-1    # follow (like tail -F)
  grain logs --qemu sbox-1

Serial:  ~/.grain/vms/<name>/serial.log
QEMU:    ~/.grain/logs/<name>.log`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			if err := requireLocalDaemon(cfg, "grain logs"); err != nil {
				return err
			}
			name, err := resolveLogsVMName(cfg, args)
			if err != nil {
				return err
			}
			path, label := logPath(cfg, name, useQEMU)
			if _, err := os.Stat(path); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("no %s log for %s (vm not started?)", label, name)
				}
				return err
			}
			out := cmd.OutOrStdout()
			if follow {
				ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				return followFile(ctx, path, out, 200*time.Millisecond)
			}
			return dumpFile(path, out)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output (like tail -F)")
	cmd.Flags().BoolVar(&useQEMU, "qemu", false, "show QEMU hypervisor log instead of guest serial")
	return cmd
}

// logPath returns the file path and short label for the selected log source.
func logPath(cfg config.Config, name string, qemu bool) (path, label string) {
	if qemu {
		return filepath.Join(cfg.DataDir, "logs", name+".log"), "qemu"
	}
	return filepath.Join(cfg.DataDir, "vms", name, "serial.log"), "serial"
}

// resolveLogsVMName picks a VM name: explicit arg, single listed VM, or error.
// Uses the daemon List API when available; falls back to scanning DataDir/vms.
func resolveLogsVMName(cfg config.Config, args []string) (string, error) {
	if len(args) >= 1 && args[0] != "" {
		return args[0], nil
	}
	// Prefer daemon list (running/known VMs).
	c, err := clientFrom(cfg)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if list, err := c.List(ctx); err == nil {
		if len(list) == 0 {
			return "", fmt.Errorf("no vms — create one first:  grain new")
		}
		if len(list) == 1 {
			return list[0].Name, nil
		}
		names := make([]string, 0, len(list))
		for _, i := range list {
			names = append(names, i.Name)
		}
		return "", fmt.Errorf("which vm? pick one: %s\n  example: grain logs %s", strings.Join(names, ", "), names[0])
	}
	// Daemon down — scan local vms dir.
	names, err := listLocalVMNames(cfg.DataDir)
	if err != nil {
		return "", fmt.Errorf("daemon not up — run: grain up (or pass a vm name: grain logs <name>)")
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no vms — create one first:  grain new")
	}
	if len(names) == 1 {
		return names[0], nil
	}
	return "", fmt.Errorf("which vm? pick one: %s\n  example: grain logs %s", strings.Join(names, ", "), names[0])
}

func listLocalVMNames(dataDir string) ([]string, error) {
	dir := filepath.Join(dataDir, "vms")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Prefer dirs that look like VMs (have meta.json).
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "meta.json")); err == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// dumpFile writes the entire file to w.
func dumpFile(path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// followFile prints existing content then polls for new data until ctx is done.
// Poll interval matches a simple tail -F style loop.
func followFile(ctx context.Context, path string, w io.Writer, interval time.Duration) error {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	var offset int64
	// Initial dump of whatever exists.
	n, err := copyFromOffset(path, offset, w)
	if err != nil {
		return err
	}
	offset += n

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			n, err := copyFromOffset(path, offset, w)
			if err != nil {
				// File may have been rotated/truncated; reset and continue.
				if os.IsNotExist(err) {
					offset = 0
					continue
				}
				// Truncation: size shrank below offset.
				if isTruncate(err) {
					offset = 0
					continue
				}
				return err
			}
			offset += n
		}
	}
}

// errTruncate is returned when the file shrank past our read offset.
type errTruncate struct{}

func (errTruncate) Error() string { return "file truncated" }

func isTruncate(err error) bool {
	_, ok := err.(errTruncate)
	return ok
}

// copyFromOffset reads path from offset to EOF into w. Returns bytes written.
// If the file is shorter than offset (truncated), returns errTruncate.
func copyFromOffset(path string, offset int64, w io.Writer) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	if size < offset {
		return 0, errTruncate{}
	}
	if size == offset {
		return 0, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, err
	}
	return io.Copy(w, f)
}

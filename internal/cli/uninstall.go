package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/spf13/cobra"
)

// cmdUninstall removes the grain CLI binary and optionally the data directory.
// Inverse of scripts/install.sh (binary + guest agent under ~/.grain/agent).
func cmdUninstall(cfgPath *string) *cobra.Command {
	var purge, yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall grain (CLI binary; optional data purge)",
		Long: `Stop the local daemon (if running) and remove the grain CLI binary.

By default, the data directory (VMs, images, keys, config) is kept. Pass
--purge to delete it as well (destructive).

Examples:
  grain uninstall              # remove binary; keep data
  grain uninstall --purge -y   # remove binary + data dir without prompt
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				// Still allow binary removal if config is broken.
				cfg = config.Defaults()
			}
			return runUninstall(cfg, purge, yes)
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the grain data directory (VMs, images, keys)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation for --purge")
	return cmd
}

func runUninstall(cfg config.Config, purge, yes bool) error {
	// Local only — do not stop a remote host daemon when CLI is in remote mode.
	if !remoteMode(cfg) {
		stopLocalDaemonBestEffort(cfg)
	}

	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate grain binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}

	if purge {
		if !yes {
			fmt.Fprintf(os.Stderr, "This will permanently delete %s (VMs, images, SSH keys, config).\n", cfg.DataDir)
			fmt.Fprint(os.Stderr, "Type 'yes' to continue: ")
			var line string
			_, _ = fmt.Scanln(&line)
			if strings.TrimSpace(strings.ToLower(line)) != "yes" {
				return fmt.Errorf("aborted")
			}
		}
		if err := removePath(cfg.DataDir, "data dir"); err != nil {
			return err
		}
	} else {
		// Keep VMs/images; drop installer's agent cache and runtime files.
		agentDir := filepath.Join(cfg.DataDir, "agent")
		if st, err := os.Stat(agentDir); err == nil && st.IsDir() {
			if err := removePath(agentDir, "guest agent dir"); err != nil {
				fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}
		}
		cleanupStaleDaemonFiles(cfg)
		_ = os.Remove(cfg.Socket)
		_ = os.Remove(daemonPIDPath(cfg))
	}

	if err := removeBinary(bin); err != nil {
		return err
	}
	fmt.Println("grain uninstalled")
	if !purge {
		fmt.Printf("  data kept at %s (use --purge to remove)\n", cfg.DataDir)
	}
	return nil
}

func stopLocalDaemonBestEffort(cfg config.Config) {
	pidPath := daemonPIDPath(cfg)
	pid, err := readPID(pidPath)
	if err != nil {
		cleanupStaleDaemonFiles(cfg)
		return
	}
	if !pidAlive(pid) {
		cleanupStaleDaemonFiles(cfg)
		return
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = p.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pidAlive(pid) {
		_ = p.Signal(syscall.SIGKILL)
	}
	cleanupStaleDaemonFiles(cfg)
	fmt.Println("stopped local daemon")
}

func removePath(path, label string) error {
	if path == "" || path == "/" || path == "." {
		return fmt.Errorf("refusing to remove %s path %q", label, path)
	}
	// Extra safety: require path to look like a grain data dir or agent subdir.
	base := filepath.Base(path)
	if base != ".grain" && base != "agent" && !strings.Contains(path, string(filepath.Separator)+".grain") {
		// Allow explicit DataDir that is not named .grain (custom data_dir).
		if label != "data dir" && label != "guest agent dir" {
			return fmt.Errorf("refusing to remove unexpected path %q", path)
		}
	}
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s %s: %w", label, path, err)
	}
	fmt.Printf("removed %s  %s\n", label, path)
	return nil
}

func removeBinary(bin string) error {
	if bin == "" {
		return fmt.Errorf("empty binary path")
	}
	if err := os.Remove(bin); err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("binary already gone  %s\n", bin)
			return nil
		}
		return fmt.Errorf("remove binary %s: %w (try: sudo grain uninstall)", bin, err)
	}
	fmt.Printf("removed binary  %s\n", bin)
	return nil
}

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/update"
	"github.com/spf13/cobra"
)

// Install script URL (override with GRAIN_INSTALL_SCRIPT for mirrors/tests).
func installScriptURL() string {
	if u := strings.TrimSpace(os.Getenv("GRAIN_INSTALL_SCRIPT")); u != "" {
		return u
	}
	return update.DefaultInstallScript
}

func cmdUpdate(cfgPath *string, version string) *cobra.Command {
	var checkOnly, force bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest grain release",
		Long: `Check GitHub Releases for a newer grain CLI, then re-run the install
script to upgrade (same path as first-time install).

  grain update           # check; install if a newer release exists
  grain update --check   # report only (exit 1 if an update is available)
  grain update --force   # re-run the installer even when already current

Upgrade notices on other commands can be disabled:

  export GRAIN_CHECK_UPDATES=0
  # or GRAIN_NO_UPDATE_CHECK=1
  # or in ~/.grain/config.yaml: check_updates: false
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				cfg = config.Defaults()
			}
			return runUpdate(cfg, version, checkOnly, force)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check for a newer release (exit 1 if available)")
	cmd.Flags().BoolVar(&force, "force", false, "re-run the install script even if already up to date")
	return cmd
}

func runUpdate(cfg config.Config, version string, checkOnly, force bool) error {
	res, err := update.Check(update.Options{
		Current:      version,
		DataDir:      cfg.DataDir,
		ForceRefresh: true,
	})
	if err != nil {
		if checkOnly {
			return fmt.Errorf("check for updates: %w", err)
		}
		// Still allow install when GitHub is unreachable.
		fmt.Fprintf(os.Stderr, "warning: could not check latest release (%v); running installer\n", err)
		return runInstallScript()
	}

	cur := update.NormalizeVersion(res.Current)
	lat := update.NormalizeVersion(res.Latest)
	fmt.Printf("current: %s\n", displayVer(res.Current))
	fmt.Printf("latest:  %s\n", displayVer(res.Latest))

	if checkOnly {
		if res.UpdateAvail {
			fmt.Fprintf(os.Stderr, "update available — run: grain update\n")
			if res.HTMLURL != "" {
				fmt.Fprintf(os.Stderr, "  %s\n", res.HTMLURL)
			}
			return exitCodeError(1)
		}
		if !update.Comparable(res.Current) {
			fmt.Fprintf(os.Stderr, "note: current version %q is not a release tag; cannot compare\n", res.Current)
		} else {
			fmt.Println("already up to date")
		}
		return nil
	}

	if !force && !res.UpdateAvail {
		if update.Comparable(res.Current) && cur == lat {
			fmt.Println("already up to date")
			return nil
		}
		if !update.Comparable(res.Current) {
			fmt.Fprintf(os.Stderr, "note: current version %q is not a release tag; re-run with --force to install latest\n", res.Current)
			return nil
		}
		// Current newer than latest (pre-release / custom) — don't downgrade.
		fmt.Println("already up to date (or newer than latest release)")
		return nil
	}

	if res.UpdateAvail {
		fmt.Printf("updating %s → %s …\n", displayVer(res.Current), displayVer(res.Latest))
	} else {
		fmt.Println("re-running installer (--force)…")
	}
	return runInstallScript()
}

func displayVer(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "(unknown)"
	}
	if strings.HasPrefix(v, "v") || v == "dev" || !update.Comparable(v) {
		return v
	}
	return "v" + update.NormalizeVersion(v)
}

func runInstallScript() error {
	url := installScriptURL()
	// Prefer curl (same as public install docs); fall back to wget.
	var shellCmd string
	if _, err := exec.LookPath("curl"); err == nil {
		shellCmd = fmt.Sprintf("curl -fsSL %q | bash", url)
	} else if _, err := exec.LookPath("wget"); err == nil {
		shellCmd = fmt.Sprintf("wget -qO- %q | bash", url)
	} else {
		return fmt.Errorf("need curl or wget to download the install script")
	}
	c := exec.Command("bash", "-c", shellCmd)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	if err := c.Run(); err != nil {
		return fmt.Errorf("install script: %w", err)
	}
	return nil
}

// updateCheckEnabled reports whether background upgrade notices are on.
// Env wins over config: GRAIN_NO_UPDATE_CHECK=1 or GRAIN_CHECK_UPDATES=0|false|off disables;
// GRAIN_CHECK_UPDATES=1|true|on enables.
func updateCheckEnabled(cfg config.Config) bool {
	if envTruthy(os.Getenv("GRAIN_NO_UPDATE_CHECK")) {
		return false
	}
	if v, ok := os.LookupEnv("GRAIN_CHECK_UPDATES"); ok && strings.TrimSpace(v) != "" {
		return envTruthy(v)
	}
	return cfg.CheckUpdates
}

func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "y":
		return true
	default:
		return false
	}
}

// maybePrintUpdateNotice writes a one-line stderr hint when a newer release is known.
// Never fails the CLI: network/cache errors are silent.
func maybePrintUpdateNotice(cfg config.Config, version, commandName string) {
	if !updateCheckEnabled(cfg) {
		return
	}
	switch commandName {
	case "update", "version", "completion", "help", "uninstall":
		return
	case "":
		return
	}
	// Prefer cache so normal commands stay offline-friendly; refresh if stale.
	res, err := update.Check(update.Options{
		Current: version,
		DataDir: cfg.DataDir,
	})
	if err != nil || !res.UpdateAvail {
		return
	}
	fmt.Fprintf(os.Stderr, "note: grain %s is available (you have %s); run: grain update\n",
		displayVer(res.Latest), displayVer(res.Current))
}

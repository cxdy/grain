package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vmsync"
	"github.com/spf13/cobra"
)

func cmdSync(cfgPath *string) *cobra.Command {
	root := &cobra.Command{
		Use:   "sync",
		Short: "Unidirectional host↔guest directory sync (agent required)",
		Long: `Incrementally sync a host directory with a guest directory via the guest agent.

  grain sync push  <HOST_DIR>       <NAME:GUEST_DIR>  [flags]
  grain sync pull  <NAME:GUEST_DIR> <HOST_DIR>        [flags]

Unlike grain cp (full snapshot replace), sync plans creates/updates/deletes,
skips unchanged paths relative to a host-side baseline, and fails on conflicts
(both sides diverged). Dest-ahead paths are kept (not overwritten) unless --force.

Requires the guest agent (no scp fallback). Works over remote CLI (GRAIN_API)
via the daemon's agent proxy. Directory roots only — use grain cp for files.

Exit codes: 0 ok, 1 usage, 2 conflicts (no apply), 3 apply error.`,
	}
	root.AddCommand(cmdSyncPush(cfgPath), cmdSyncPull(cfgPath))
	return root
}

type syncFlags struct {
	delete        bool
	dryRun        bool
	force         bool
	checksum      bool
	exclude       []string
	noDefaults    bool
	noGitignore   bool
	noGrainignore bool
	verbose       bool
	maxFileSize   int64
}

func bindSyncFlags(cmd *cobra.Command, f *syncFlags) {
	cmd.Flags().BoolVar(&f.delete, "delete", false, "remove dest paths missing on source (ignored paths never deleted)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print plan only; no writes or state update")
	cmd.Flags().BoolVar(&f.force, "force", false, "source-wins for conflicts and dest-ahead paths")
	cmd.Flags().BoolVar(&f.checksum, "checksum", false, "reserved: content hash refine (not yet active)")
	cmd.Flags().StringArrayVar(&f.exclude, "exclude", nil, "extra gitignore-style patterns (repeatable)")
	cmd.Flags().BoolVar(&f.noDefaults, "no-defaults", false, "do not apply built-in ignores (.git/)")
	cmd.Flags().BoolVar(&f.noGitignore, "no-gitignore", false, "do not load host .gitignore")
	cmd.Flags().BoolVar(&f.noGrainignore, "no-grainignore", false, "do not load host .grainignore")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "list skipped and kept_dest paths")
	cmd.Flags().Int64Var(&f.maxFileSize, "max-file-size", 0, "skip source files larger than N bytes (0=unlimited)")
}

func cmdSyncPush(cfgPath *string) *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "push <HOST_DIR> <NAME:GUEST_DIR>",
		Short: "Sync host directory → guest directory",
		Example: `  grain sync push ~/proj lab:/work/proj
  grain sync push ./src vm:/work/ --dry-run
  grain sync push ~/proj vm:/work/ --delete --exclude 'node_modules/'`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncCmd(cfgPath, vmsync.Push, args[0], args[1], f)
		},
	}
	bindSyncFlags(cmd, &f)
	return cmd
}

func cmdSyncPull(cfgPath *string) *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "pull <NAME:GUEST_DIR> <HOST_DIR>",
		Short: "Sync guest directory → host directory",
		Example: `  grain sync pull lab:/work/proj ~/proj
  grain sync pull vm:/work/ ./out --dry-run
  grain sync pull vm:/work/ ~/proj --force`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncCmd(cfgPath, vmsync.Pull, args[0], args[1], f)
		},
	}
	bindSyncFlags(cmd, &f)
	return cmd
}

func runSyncCmd(cfgPath *string, verb vmsync.Verb, arg0, arg1 string, f syncFlags) error {
	hostPath, vm, guestPath, err := vmsync.ParseArgs(verb, arg0, arg1, func(s string) (bool, string, string) {
		spec := parseCPSpec(s)
		return spec.Guest, spec.Name, spec.Path
	})
	if err != nil {
		return exitCodeError(vmsync.ExitUsage).with(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(guestPath), "/") {
		return exitCodeError(vmsync.ExitUsage).with(fmt.Errorf("sync: guest path must be absolute (got %q)", guestPath))
	}

	cfg, err := loadCfg(cfgPath)
	if err != nil {
		return err
	}
	c, err := clientFrom(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fs, err := openSyncFS(ctx, cfg, c, vm)
	if err != nil {
		return err
	}

	apiID := syncAPIIdentity(cfg)
	res, err := vmsync.Run(ctx, vmsync.Options{
		Verb:          verb,
		VM:            vm,
		HostRoot:      hostPath,
		GuestRoot:     guestPath,
		APIIdentity:   apiID,
		DataDir:       cfg.DataDir,
		FS:            fs,
		Out:           os.Stdout,
		ErrOut:        os.Stderr,
		Delete:        f.delete,
		DryRun:        f.dryRun,
		Force:         f.force,
		Checksum:      f.checksum,
		Exclude:       f.exclude,
		NoDefaults:    f.noDefaults,
		NoGitignore:   f.noGitignore,
		NoGrainignore: f.noGrainignore,
		Verbose:       f.verbose,
		MaxFileSize:   f.maxFileSize,
	})
	if res != nil && res.ExitCode != vmsync.ExitOK {
		if err != nil {
			return exitCodeError(res.ExitCode).with(err)
		}
		return exitCodeError(res.ExitCode)
	}
	if err != nil {
		code := vmsync.ExitApply
		if errors.Is(err, vmsync.ErrConflicts) {
			code = vmsync.ExitConflict
		}
		return exitCodeError(code).with(err)
	}
	return nil
}

// exitWith wraps an error with an exit code.
type exitWith struct {
	code int
	err  error
}

func (e exitCodeError) with(err error) error {
	if err == nil {
		return e
	}
	return &exitWith{code: int(e), err: err}
}

func (e *exitWith) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("exit status %d", e.code)
}

func (e *exitWith) ExitCode() int { return e.code }

func (e *exitWith) Unwrap() error { return e.err }

func openSyncFS(ctx context.Context, cfg config.Config, c *api.Client, vm string) (vmsync.FS, error) {
	_ = ctx
	viaDaemon := remoteMode(cfg)
	if viaDaemon {
		// Probe agent via daemon-proxied health when possible.
		return vmsync.NewAPIFS(c, vm), nil
	}
	ac, err := dialGuestAgent(c, vm, true /* force — no scp */)
	if err != nil {
		return nil, fmt.Errorf("sync: agent not available for %s (try: grain agent health %s): %w", vm, vm, err)
	}
	return vmsync.NewAgentFS(ac), nil
}

func syncAPIIdentity(cfg config.Config) string {
	if u := effectiveAPIURL(cfg); u != "" {
		return u
	}
	return "local:" + cfg.Socket
}

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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

By default equality uses size+mtime fingerprints. Pass --checksum to re-hash
file content (SHA-256) for paths the planner would skip, so same-size/mtime
files with different content still transfer.

Pass --watch to re-run sync on an interval (default 2s) until interrupted
(Ctrl+C). Conflicts and apply errors are printed and the loop continues;
usage errors stop. Incompatible with --dry-run.

Requires the guest agent (no scp fallback). Works over remote CLI (GRAIN_API)
via the daemon's agent proxy. Directory roots only — use grain cp for files.

Exit codes: 0 ok, 1 usage, 2 conflicts (no apply), 3 apply error.
With --watch, interrupt exits 0; only usage errors abort the loop.`,
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
	watch         bool
	interval      time.Duration
}

func bindSyncFlags(cmd *cobra.Command, f *syncFlags) {
	cmd.Flags().BoolVar(&f.delete, "delete", false, "remove dest paths missing on source (ignored paths never deleted)")
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "print plan only; no writes or state update")
	cmd.Flags().BoolVar(&f.force, "force", false, "source-wins for conflicts and dest-ahead paths")
	cmd.Flags().BoolVar(&f.checksum, "checksum", false, "re-compare skipped files with SHA-256 content hashes (upgrade skip→update when content differs)")
	cmd.Flags().StringArrayVar(&f.exclude, "exclude", nil, "extra gitignore-style patterns (repeatable)")
	cmd.Flags().BoolVar(&f.noDefaults, "no-defaults", false, "do not apply built-in ignores (.git/)")
	cmd.Flags().BoolVar(&f.noGitignore, "no-gitignore", false, "do not load host .gitignore")
	cmd.Flags().BoolVar(&f.noGrainignore, "no-grainignore", false, "do not load host .grainignore")
	cmd.Flags().BoolVarP(&f.verbose, "verbose", "v", false, "list skipped and kept_dest paths")
	cmd.Flags().Int64Var(&f.maxFileSize, "max-file-size", 0, "skip source files larger than N bytes (0=unlimited)")
	cmd.Flags().BoolVar(&f.watch, "watch", false, "re-run sync on --interval until interrupted (Ctrl+C)")
	cmd.Flags().DurationVar(&f.interval, "interval", 2*time.Second, "poll interval between sync runs when --watch is set")
}

func cmdSyncPush(cfgPath *string) *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "push <HOST_DIR> <NAME:GUEST_DIR>",
		Short: "Sync host directory → guest directory",
		Example: `  grain sync push ~/proj lab:/work/proj
  grain sync push ./src vm:/work/ --dry-run
  grain sync push ~/proj vm:/work/ --delete --exclude 'node_modules/'
  grain sync push ~/proj lab:/work/proj --watch
  grain sync push ~/proj lab:/work/proj --watch --interval 5s`,
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
  grain sync pull vm:/work/ ~/proj --force
  grain sync pull lab:/work/proj ~/proj --watch --interval 3s`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncCmd(cfgPath, vmsync.Pull, args[0], args[1], f)
		},
	}
	bindSyncFlags(cmd, &f)
	return cmd
}

func runSyncCmd(cfgPath *string, verb vmsync.Verb, arg0, arg1 string, f syncFlags) error {
	if f.watch && f.dryRun {
		return exitCodeError(vmsync.ExitUsage).with(fmt.Errorf("sync: --dry-run and --watch are incompatible"))
	}

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
	label := "sync push"
	if verb == vmsync.Pull {
		label = "sync pull"
	}

	opts := vmsync.Options{
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
	}

	once := func(ctx context.Context) (int, error) {
		return runSyncOnce(ctx, opts, label, f.dryRun)
	}

	if f.watch {
		return runSyncWatch(ctx, f.interval, once)
	}
	code, err := once(ctx)
	if code != vmsync.ExitOK {
		if err != nil {
			return exitCodeError(code).with(err)
		}
		return exitCodeError(code)
	}
	return err
}

// runSyncOnce executes a single vmsync.Run and finishes progress. Returns the
// process exit code and any error (not wrapped in exitCodeError).
func runSyncOnce(ctx context.Context, opts vmsync.Options, label string, dryRun bool) (int, error) {
	prog := newTransferProgress(label)
	opts.OnProgress = syncProgressHook(prog)
	res, err := vmsync.Run(ctx, opts)
	if res != nil && res.ExitCode != vmsync.ExitOK {
		prog.Finish("")
		return res.ExitCode, err
	}
	if err != nil {
		prog.Finish("")
		code := vmsync.ExitApply
		if errors.Is(err, vmsync.ErrConflicts) {
			code = vmsync.ExitConflict
		}
		return code, err
	}
	applied := 0
	if res != nil {
		applied = res.Applied
	}
	if dryRun {
		prog.Finish(label + ": dry-run complete")
	} else {
		prog.Finish(fmt.Sprintf("%s: applied %d change(s)", label, applied))
	}
	return vmsync.ExitOK, nil
}

// syncOnceFunc runs one full sync iteration. Returns exit code and optional error.
type syncOnceFunc func(ctx context.Context) (code int, err error)

// runSyncWatch re-runs once on interval until ctx is cancelled or a usage error.
//
//	ExitOK       — print already done by once; sleep; loop
//	ExitConflict — print err if any; sleep; continue
//	ExitApply    — print err if any; sleep; continue (transient-friendly)
//	ExitUsage    — return wrapped exit error (abort)
//	ctx cancel   — return nil (interrupt → exit 0)
func runSyncWatch(ctx context.Context, interval time.Duration, once syncOnceFunc) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		code, err := once(ctx)
		if ctx.Err() != nil {
			// Interrupted mid-run: exit 0.
			return nil
		}
		switch code {
		case vmsync.ExitOK:
			// summary printed by once
		case vmsync.ExitUsage:
			if err != nil {
				return exitCodeError(code).with(err)
			}
			return exitCodeError(code)
		default:
			// Conflict / apply / unknown: surface message and keep watching.
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func syncProgressHook(prog *transferProgress) func(vmsync.ProgressEvent) {
	if prog == nil {
		return nil
	}
	return func(ev vmsync.ProgressEvent) {
		detail := ev.Phase
		if ev.Total > 0 && ev.Index > 0 {
			detail = fmt.Sprintf("%s %d/%d", ev.Phase, ev.Index, ev.Total)
		}
		if ev.Action != "" && ev.Action != ev.Phase {
			detail += " " + ev.Action
		}
		if ev.RelPath != "" {
			detail += " " + ev.RelPath
		}
		prog.SetDetail(detail)
	}
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

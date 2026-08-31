package vmsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Verb is push (host→guest) or pull (guest→host).
type Verb = syncVerb

const (
	// Push copies host → guest.
	Push = syncPush
	// Pull copies guest → host.
	Pull = syncPull
	// Both copies host-ahead paths to the guest and guest-ahead paths to the host.
	Both = syncBoth
)

// ProgressEvent is emitted during apply (and optionally inventory) for CLI UIs.
type ProgressEvent struct {
	// Phase is a short stage label: inventory, delete, mkdir, put, get, chmod, done.
	Phase string
	// Index is 1-based op index within Total (0 when unknown).
	Index int
	// Total is the number of apply ops (deletes + dir creates + file ops).
	Total int
	// RelPath is the path relative to the sync root.
	RelPath string
	// Action is the plan action (create, update, delete, …).
	Action string
}

// Options configures a sync run.
type Options struct {
	Verb      Verb
	VM        string
	HostRoot  string // absolute host directory
	GuestRoot string // absolute guest directory (slash path)
	// APIIdentity labels the state file (e.g. "local:/path.sock" or API URL).
	APIIdentity string
	// DataDir is cfg.DataDir for state files under DataDir/sync/.
	DataDir string
	// FS is the guest filesystem (required).
	FS FS
	// Out receives human-readable plan/summary lines (optional; defaults to discard).
	Out io.Writer
	// ErrOut receives warnings (optional; defaults to discard).
	ErrOut io.Writer
	// OnProgress is called during apply for live status (optional).
	OnProgress func(ProgressEvent)

	Delete bool
	DryRun bool
	Force  bool
	// Checksum enables content-hash refine: after size/mtime classification,
	// files that would be skipped are re-compared via SHA-256 on host and guest.
	// Differing hashes upgrade skip → update.
	Checksum      bool
	Exclude       []string
	NoDefaults    bool
	NoGitignore   bool
	NoGrainignore bool
	Verbose       bool
	MaxFileSize   int64 // 0 = unlimited; skip source files larger than this
}

// Result is the outcome of a sync run.
type Result struct {
	Plan     *syncPlan
	Applied  int
	ExitCode int
	// StatePath is where baseline was (or would be) written.
	StatePath string
}

// Run executes inventory → classify → apply (or dry-run) for one push/pull.
func Run(ctx context.Context, opts Options) (*Result, error) {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	errOut := opts.ErrOut
	if errOut == nil {
		errOut = io.Discard
	}
	res := &Result{ExitCode: ExitOK}

	if opts.FS == nil {
		res.ExitCode = ExitUsage
		return res, fmt.Errorf("sync: guest FS required")
	}
	if opts.VM == "" {
		res.ExitCode = ExitUsage
		return res, fmt.Errorf("sync: VM name required")
	}
	if opts.Verb != Push && opts.Verb != Pull && opts.Verb != Both {
		res.ExitCode = ExitUsage
		return res, fmt.Errorf("sync: verb must be push, pull, or both")
	}

	hostRoot, err := filepath.Abs(opts.HostRoot)
	if err != nil {
		res.ExitCode = ExitUsage
		return res, fmt.Errorf("host path: %w", err)
	}
	guestRoot := path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(opts.GuestRoot), "/"))
	if guestRoot == "" || guestRoot == "." {
		res.ExitCode = ExitUsage
		return res, fmt.Errorf("guest path must be absolute")
	}

	// Resolve roots: directory-only; source must exist.
	if err := validateHostRoot(hostRoot, opts.Verb); err != nil {
		res.ExitCode = ExitUsage
		return res, err
	}
	if err := validateGuestRoot(ctx, opts.FS, guestRoot, opts.Verb); err != nil {
		res.ExitCode = ExitUsage
		return res, err
	}
	if opts.Verb == Both {
		if err := validateBothRoots(ctx, opts.FS, hostRoot, guestRoot); err != nil {
			res.ExitCode = ExitUsage
			return res, err
		}
	}

	// Ensure dest root exists (or will be created).
	if err := ensureDestRoot(ctx, opts, hostRoot, guestRoot); err != nil {
		res.ExitCode = ExitApply
		return res, err
	}

	ign, err := buildSyncIgnore(syncIgnoreOpts{
		HostRoot:      hostRoot,
		NoDefaults:    opts.NoDefaults,
		NoGrainignore: opts.NoGrainignore,
		NoGitignore:   opts.NoGitignore,
		Exclude:       opts.Exclude,
	})
	if err != nil {
		res.ExitCode = ExitUsage
		return res, err
	}

	hostInv, err := InventoryHost(hostRoot, ign)
	if err != nil {
		res.ExitCode = ExitApply
		return res, err
	}
	guestInv, err := InventoryGuest(ctx, opts.FS, guestRoot, ign)
	if err != nil {
		res.ExitCode = ExitApply
		return res, err
	}

	if opts.MaxFileSize > 0 {
		filterMaxSize(hostInv, guestInv, opts, errOut)
	}

	apiID := opts.APIIdentity
	if apiID == "" {
		apiID = "local"
	}
	id := syncStateID(apiID, opts.VM, hostRoot, guestRoot)
	statePath := syncStatePath(opts.DataDir, id)
	res.StatePath = statePath

	st, err := loadSyncState(statePath)
	if err != nil {
		res.ExitCode = ExitApply
		return res, err
	}
	if st == nil {
		st = newSyncState(apiID, opts.VM, hostRoot, guestRoot)
	}

	plan := classifyAll(hostInv, guestInv, st, ign, syncClassifyOpts{
		Verb:     opts.Verb,
		Delete:   opts.Delete,
		Force:    opts.Force,
		Checksum: opts.Checksum,
	})
	if opts.Checksum {
		if err := refinePlanChecksum(ctx, plan, opts, hostRoot, guestRoot); err != nil {
			res.Plan = plan
			res.ExitCode = ExitApply
			return res, err
		}
		// Record that this run used content-hash refine (state metadata).
		st.Fingerprint = "checksum"
	}
	res.Plan = plan

	printPlanSummary(out, errOut, plan, opts)

	if opts.DryRun {
		if plan.HasConflicts() {
			res.ExitCode = ExitConflict
			return res, ErrConflicts
		}
		return res, nil
	}

	if plan.HasConflicts() {
		res.ExitCode = ExitConflict
		return res, ErrConflicts
	}

	applyRes, err := applySyncPlan(ctx, plan, syncApplyOpts{
		Verb:       opts.Verb,
		HostRoot:   hostRoot,
		GuestRoot:  guestRoot,
		FS:         opts.FS,
		State:      st,
		StatePath:  statePath,
		OnProgress: opts.OnProgress,
	})
	if applyRes != nil {
		res.Applied = applyRes.Applied
		res.ExitCode = applyRes.ExitCode
	}
	if err != nil {
		if errors.Is(err, ErrConflicts) {
			res.ExitCode = ExitConflict
		} else if res.ExitCode == ExitOK {
			res.ExitCode = ExitApply
		}
		return res, err
	}
	// Plan summary (counts + kept_dest) already printed via printPlanSummary.
	return res, nil
}

func validateHostRoot(hostRoot string, verb Verb) error {
	fi, err := os.Lstat(hostRoot)
	if verb == Push {
		// Host is source: must exist and be a directory.
		if err != nil {
			return fmt.Errorf("sync: host source does not exist: %s", hostRoot)
		}
		if !fi.IsDir() {
			return fmt.Errorf("sync requires directory roots (use grain cp for single files): host %s", hostRoot)
		}
		return nil
	}
	// Pull / both: host may be missing (created) or must be a directory.
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("host path: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("sync requires directory roots (use grain cp for single files): host %s", hostRoot)
	}
	return nil
}

func validateGuestRoot(ctx context.Context, gfs FS, guestRoot string, verb Verb) error {
	st, err := gfs.Stat(ctx, guestRoot)
	if verb == Pull {
		// Guest is source: must exist and be a directory.
		if err != nil {
			return fmt.Errorf("sync: guest source does not exist: %s", guestRoot)
		}
		if st.Type != "directory" {
			return fmt.Errorf("sync requires directory roots (use grain cp for single files): guest %s is %s", guestRoot, st.Type)
		}
		return nil
	}
	// Push / both: guest may be missing or must be a directory.
	if err != nil {
		return nil
	}
	if st.Type != "directory" {
		return fmt.Errorf("sync requires directory roots (use grain cp for single files): guest %s is %s", guestRoot, st.Type)
	}
	return nil
}

func ensureDestRoot(ctx context.Context, opts Options, hostRoot, guestRoot string) error {
	if opts.DryRun {
		return nil
	}
	switch opts.Verb {
	case Push:
		// Create guest dest dir if missing.
		if _, err := opts.FS.Stat(ctx, guestRoot); err != nil {
			return opts.FS.Mkdir(ctx, guestRoot, true, "0755")
		}
	case Pull:
		if _, err := os.Lstat(hostRoot); err != nil {
			if os.IsNotExist(err) {
				return os.MkdirAll(hostRoot, 0o755)
			}
			return err
		}
	case Both:
		if _, err := os.Lstat(hostRoot); err != nil && os.IsNotExist(err) {
			if err := os.MkdirAll(hostRoot, 0o755); err != nil {
				return err
			}
		}
		if _, err := opts.FS.Stat(ctx, guestRoot); err != nil {
			return opts.FS.Mkdir(ctx, guestRoot, true, "0755")
		}
	}
	return nil
}

func validateBothRoots(ctx context.Context, gfs FS, hostRoot, guestRoot string) error {
	_, hostErr := os.Lstat(hostRoot)
	hostOK := hostErr == nil
	_, guestErr := gfs.Stat(ctx, guestRoot)
	guestOK := guestErr == nil
	if !hostOK && !guestOK {
		return fmt.Errorf("sync: host or guest directory must exist (%s or %s)", hostRoot, guestRoot)
	}
	return nil
}

func filterMaxSize(host, guest map[string]*syncInvEntry, opts Options, errOut io.Writer) {
	filter := func(inv map[string]*syncInvEntry) {
		for rel, e := range inv {
			if e == nil || e.Type != "file" {
				continue
			}
			if e.Size > opts.MaxFileSize {
				fmt.Fprintf(errOut, "sync: skip %s: size %d exceeds --max-file-size %d\n", rel, e.Size, opts.MaxFileSize)
				delete(inv, rel)
			}
		}
	}
	switch opts.Verb {
	case Pull:
		filter(guest)
	case Both:
		filter(host)
		filter(guest)
	default:
		filter(host)
	}
}

func printPlanSummary(out, errOut io.Writer, plan *syncPlan, opts Options) {
	if plan == nil {
		return
	}
	for _, it := range plan.Items {
		switch it.Action {
		case syncActConflict:
			fmt.Fprintf(errOut, "conflict: %s (%s)\n", it.RelPath, it.Reason)
		case syncActCreate, syncActUpdate, syncActReplace, syncActUpdateMode, syncActDelete:
			fmt.Fprintf(out, "%s %s\n", it.Action, it.RelPath)
		case syncActKeptDest:
			if opts.Verbose {
				fmt.Fprintf(out, "kept_dest %s\n", it.RelPath)
			}
		case syncActSkip:
			if opts.Verbose {
				fmt.Fprintf(out, "skip %s (%s)\n", it.RelPath, it.Reason)
			}
		}
	}
	// Always print summary counts (dry-run and apply) so agents/humans see
	// kept_dest and conflicts without requiring -v.
	fmt.Fprintf(out, "sync: created=%d updated=%d deleted=%d skipped=%d kept_dest=%d conflicts=%d\n",
		plan.Created, plan.Updated, plan.Deleted, plan.Skipped, plan.KeptDest, plan.Conflicts)
	if plan.KeptDest > 0 {
		fmt.Fprintf(out, "sync: %d path(s) kept on dest (use -v to list; --force to overwrite)\n", plan.KeptDest)
	}
	if plan.Conflicts > 0 {
		fmt.Fprintf(errOut, "sync: %d conflict(s) — resolve or pass --force (no changes applied)\n", plan.Conflicts)
	}
}

// ParseArgs validates push/pull/both argument shapes.
//
//	push: hostDir, NAME:guestDir
//	pull: NAME:guestDir, hostDir  OR  hostDir, NAME:guestDir
//	both: either order (one host path, one NAME:guestDir)
//
// parseGuest reports whether s is NAME:path form.
func ParseArgs(verb Verb, arg0, arg1 string, parseGuest func(string) (guest bool, name, path string)) (hostPath, vm, guestPath string, err error) {
	g0, n0, p0 := parseGuest(arg0)
	g1, n1, p1 := parseGuest(arg1)
	switch verb {
	case Push:
		if g0 {
			return "", "", "", fmt.Errorf("sync push: first arg must be a host directory (got guest %s)", arg0)
		}
		if !g1 {
			return "", "", "", fmt.Errorf("sync push: second arg must be NAME:GUEST_DIR (got %s)", arg1)
		}
		if strings.TrimSpace(p1) == "" || p1 == "." {
			return "", "", "", fmt.Errorf("sync push: guest path required (NAME:/path)")
		}
		return arg0, n1, p1, nil
	case Pull, Both:
		label := "sync pull"
		if verb == Both {
			label = "sync"
		}
		return parseSyncPair(label, arg0, arg1, g0, n0, p0, g1, n1, p1)
	default:
		return "", "", "", fmt.Errorf("unknown sync verb %q", verb)
	}
}

func parseSyncPair(label, arg0, arg1 string, g0 bool, n0, p0 string, g1 bool, n1, p1 string) (hostPath, vm, guestPath string, err error) {
	switch {
	case !g0 && g1:
		if strings.TrimSpace(p1) == "" || p1 == "." {
			return "", "", "", fmt.Errorf("%s: guest path required (NAME:/path)", label)
		}
		return arg0, n1, p1, nil
	case g0 && !g1:
		if strings.TrimSpace(p0) == "" || p0 == "." {
			return "", "", "", fmt.Errorf("%s: guest path required (NAME:/path)", label)
		}
		return arg1, n0, p0, nil
	case g0 && g1:
		return "", "", "", fmt.Errorf("%s: one arg must be a host directory (got two guest paths)", label)
	default:
		return "", "", "", fmt.Errorf("%s: one arg must be NAME:GUEST_DIR (got %s %s)", label, arg0, arg1)
	}
}

package vmsync

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cxdy/grain/internal/agent"
)

// Exit codes (design doc).
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitConflict = 2
	ExitApply    = 3

	syncExitOK       = ExitOK
	syncExitUsage    = ExitUsage
	syncExitConflict = ExitConflict
	syncExitApply    = ExitApply
)

// ErrConflicts is returned when the plan has conflicts and apply is blocked.
var ErrConflicts = fmt.Errorf("sync: conflicts present")

// errSyncConflicts is the legacy alias used inside apply.
var errSyncConflicts = ErrConflicts

// syncApplyOpts configures applySyncPlan.
type syncApplyOpts struct {
	Verb      syncVerb
	HostRoot  string // absolute host directory root
	GuestRoot string // absolute guest directory root (slash path)
	FS        syncFS
	// State is the in-memory baseline document (mutated during apply).
	// Nil is treated as empty new state (caller should seed metadata).
	State *syncState
	// StatePath is where to persist after full success when Dirty.
	// Empty disables disk write (tests).
	StatePath string
	// OnProgress reports per-op apply progress (optional).
	OnProgress func(ProgressEvent)
}

// syncApplyResult summarizes apply outcomes for the CLI.
type syncApplyResult struct {
	Dirty    bool
	Applied  int // create/update/delete/update_mode/replace ops executed
	ExitCode int
}

// safeRelJoin joins host root + slash-separated rel. Rejects escapes.
func safeRelJoin(root, rel string) (string, error) {
	root = filepath.Clean(root)
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	// Reject absolute before stripping separators.
	if rel != "" && (path.IsAbs(rel) || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/")) {
		return "", fmt.Errorf("illegal sync path: absolute %q", rel)
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." {
		return root, nil
	}
	clean := path.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("illegal sync path: %q", rel)
	}
	// Convert to OS path segments without introducing ..
	osRel := filepath.FromSlash(clean)
	if osRel == ".." || strings.HasPrefix(osRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("illegal sync path: %q", rel)
	}
	target := filepath.Join(root, osRel)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+sep) {
		return "", fmt.Errorf("illegal sync path: %q escapes root", rel)
	}
	return absTarget, nil
}

// safeGuestJoin joins guest root + rel into a clean absolute guest path.
func safeGuestJoin(guestRoot, rel string) (string, error) {
	guestRoot = path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(guestRoot), "/"))
	if guestRoot == "/" {
		// allow root as sync root
	} else if !strings.HasPrefix(guestRoot, "/") {
		return "", fmt.Errorf("guest root must be absolute: %q", guestRoot)
	}
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel != "" && (path.IsAbs(rel) || strings.HasPrefix(rel, "/")) {
		return "", fmt.Errorf("illegal guest rel path: absolute %q", rel)
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || rel == "." {
		return guestRoot, nil
	}
	clean := path.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("illegal guest rel path: %q", rel)
	}
	joined := path.Join(guestRoot, clean)
	// Ensure still under guestRoot.
	if guestRoot != "/" && joined != guestRoot && !strings.HasPrefix(joined, guestRoot+"/") {
		return "", fmt.Errorf("illegal guest rel path: %q escapes root", rel)
	}
	return joined, nil
}

// applySyncPlan executes a plan against host + guest.
//
// Conflict barrier: if plan.HasConflicts(), returns errSyncConflicts with no writes
// and no state file update (crash model A).
//
// On any apply error, returns that error; on-disk StatePath is left unchanged.
// On full success, writes StatePath iff State was dirtied.
func applySyncPlan(ctx context.Context, plan *syncPlan, opts syncApplyOpts) (*syncApplyResult, error) {
	res := &syncApplyResult{ExitCode: syncExitOK}
	if plan == nil {
		return res, fmt.Errorf("sync: nil plan")
	}
	if opts.FS == nil {
		return res, fmt.Errorf("sync: nil guest FS")
	}
	if opts.HostRoot == "" || opts.GuestRoot == "" {
		return res, fmt.Errorf("sync: host and guest roots required")
	}
	if plan.HasConflicts() {
		res.ExitCode = syncExitConflict
		return res, errSyncConflicts
	}
	if opts.State == nil {
		opts.State = newSyncState("", "", opts.HostRoot, opts.GuestRoot)
	}

	// Partition ops.
	var deletes, dirCreates, fileOps []syncPlanItem
	for _, it := range plan.Items {
		switch it.Action {
		case syncActDelete:
			deletes = append(deletes, it)
		case syncActCreate:
			if it.Source != nil && it.Source.Type == "directory" {
				dirCreates = append(dirCreates, it)
			} else {
				fileOps = append(fileOps, it)
			}
		case syncActUpdate, syncActReplace, syncActUpdateMode:
			fileOps = append(fileOps, it)
		case syncActSkip:
			if it.BaselineDirty {
				// Cold-start size match: record baseline without transfer.
				markColdStartBaseline(opts, it)
				res.Dirty = true
			}
		case syncActKeptDest, syncActConflict:
			// kept_dest: no state write; conflict already barred
		}
	}

	totalOps := len(deletes) + len(dirCreates) + len(fileOps)
	opIndex := 0
	report := func(phase string, it syncPlanItem) {
		if opts.OnProgress == nil {
			return
		}
		opts.OnProgress(ProgressEvent{
			Phase:   phase,
			Index:   opIndex,
			Total:   totalOps,
			RelPath: it.RelPath,
			Action:  string(it.Action),
		})
	}

	// 1. Deletes deepest first.
	sort.Slice(deletes, func(i, j int) bool {
		return depthKey(deletes[i].RelPath) > depthKey(deletes[j].RelPath)
	})
	for _, it := range deletes {
		if err := ctx.Err(); err != nil {
			res.ExitCode = syncExitApply
			return res, err
		}
		opIndex++
		report("delete", it)
		if err := applyDelete(ctx, opts, it); err != nil {
			res.ExitCode = syncExitApply
			return res, err
		}
		opts.State.removeEntry(it.RelPath)
		res.Dirty = true
		res.Applied++
	}

	// 2. Directory creates — shallow first.
	sort.Slice(dirCreates, func(i, j int) bool {
		return depthKey(dirCreates[i].RelPath) < depthKey(dirCreates[j].RelPath)
	})
	for _, it := range dirCreates {
		if err := ctx.Err(); err != nil {
			res.ExitCode = syncExitApply
			return res, err
		}
		opIndex++
		report("mkdir", it)
		if err := applyDirCreate(ctx, opts, it); err != nil {
			res.ExitCode = syncExitApply
			return res, err
		}
		if err := refreshBaselineBoth(ctx, opts, it.RelPath, it); err != nil {
			res.ExitCode = syncExitApply
			return res, err
		}
		res.Dirty = true
		res.Applied++
	}

	// 3. File ops — sorted by path, concurrency 1.
	sort.Slice(fileOps, func(i, j int) bool {
		return fileOps[i].RelPath < fileOps[j].RelPath
	})
	for _, it := range fileOps {
		if err := ctx.Err(); err != nil {
			res.ExitCode = syncExitApply
			return res, err
		}
		opIndex++
		phase := "put"
		if opts.Verb == syncPull {
			phase = "get"
		}
		if it.Action == syncActUpdateMode {
			phase = "chmod"
		}
		report(phase, it)
		if err := applyFileOp(ctx, opts, it); err != nil {
			res.ExitCode = syncExitApply
			return res, err
		}
		res.Dirty = true
		res.Applied++
	}

	// Crash model A: persist only after full success.
	if res.Dirty && opts.StatePath != "" {
		if err := saveSyncState(opts.StatePath, opts.State); err != nil {
			res.ExitCode = syncExitApply
			return res, fmt.Errorf("sync state save: %w", err)
		}
	}
	return res, nil
}

func depthKey(rel string) int {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return 0
	}
	return strings.Count(rel, "/") + 1
}

func markColdStartBaseline(opts syncApplyOpts, it syncPlanItem) {
	var hostE, guestE *syncInvEntry
	switch opts.Verb {
	case syncPull:
		// S=guest, D=host
		guestE, hostE = it.Source, it.Dest
	default:
		hostE, guestE = it.Source, it.Dest
	}
	opts.State.setEntry(it.RelPath, invToFingerprint(hostE), invToFingerprint(guestE))
}

func applyDelete(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	switch opts.Verb {
	case syncPull:
		// Dest is host.
		hp, err := safeRelJoin(opts.HostRoot, it.RelPath)
		if err != nil {
			return err
		}
		return os.RemoveAll(hp)
	default:
		// Dest is guest.
		gp, err := safeGuestJoin(opts.GuestRoot, it.RelPath)
		if err != nil {
			return err
		}
		return opts.FS.Remove(ctx, gp, true)
	}
}

func applyDirCreate(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	mode := "0755"
	if it.Source != nil && it.Source.Mode != "" {
		mode = normalizeSyncMode(it.Source.Mode)
	}
	switch opts.Verb {
	case syncPull:
		hp, err := safeRelJoin(opts.HostRoot, it.RelPath)
		if err != nil {
			return err
		}
		perm := parseOctalMode(mode, 0o755)
		return os.MkdirAll(hp, perm)
	default:
		gp, err := safeGuestJoin(opts.GuestRoot, it.RelPath)
		if err != nil {
			return err
		}
		return opts.FS.Mkdir(ctx, gp, true, mode)
	}
}

func applyFileOp(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	switch it.Action {
	case syncActUpdateMode:
		return applyUpdateMode(ctx, opts, it)
	case syncActCreate, syncActUpdate, syncActReplace:
		return applyFileTransfer(ctx, opts, it)
	default:
		return fmt.Errorf("sync: unexpected file op %s for %s", it.Action, it.RelPath)
	}
}

func applyUpdateMode(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	mode := "0644"
	if it.Source != nil && it.Source.Mode != "" {
		mode = normalizeSyncMode(it.Source.Mode)
	}
	switch opts.Verb {
	case syncPull:
		hp, err := safeRelJoin(opts.HostRoot, it.RelPath)
		if err != nil {
			return err
		}
		return os.Chmod(hp, parseOctalMode(mode, 0o644))
	default:
		// Re-Put content with new mode (no guest chmod API beyond Put).
		return applyFileTransfer(ctx, opts, it)
	}
}

func applyFileTransfer(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	if it.Source != nil && it.Source.Type == "symlink" {
		return applySymlinkTransfer(ctx, opts, it)
	}
	switch opts.Verb {
	case syncPush:
		return applyPushFile(ctx, opts, it)
	case syncPull:
		return applyPullFile(ctx, opts, it)
	default:
		return fmt.Errorf("sync: unknown verb %q", opts.Verb)
	}
}

func applySymlinkTransfer(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	target := ""
	if it.Source != nil {
		target = it.Source.Target
	}
	switch opts.Verb {
	case syncPush:
		return applyPushSymlink(ctx, opts, it, target)
	case syncPull:
		return applyPullSymlink(ctx, opts, it, target)
	default:
		return fmt.Errorf("sync: unknown verb %q", opts.Verb)
	}
}

func applyPushSymlink(ctx context.Context, opts syncApplyOpts, it syncPlanItem, target string) error {
	hp, err := safeRelJoin(opts.HostRoot, it.RelPath)
	if err != nil {
		return err
	}
	gp, err := safeGuestJoin(opts.GuestRoot, it.RelPath)
	if err != nil {
		return err
	}
	if target == "" {
		t, err := os.Readlink(hp)
		if err != nil {
			return fmt.Errorf("host readlink %s: %w", it.RelPath, err)
		}
		target = t
	}
	if target == "" {
		return fmt.Errorf("push symlink %s: empty target", it.RelPath)
	}
	if err := opts.FS.Symlink(ctx, gp, target); err != nil {
		return fmt.Errorf("symlink %s: %w", gp, err)
	}
	hfi, err := os.Lstat(hp)
	if err != nil {
		return fmt.Errorf("host %s: %w", it.RelPath, err)
	}
	hostFP := lstatToFingerprint(hp, hfi)
	gInfo, err := opts.FS.Stat(ctx, gp)
	if err != nil {
		return fmt.Errorf("stat guest %s: %w", gp, err)
	}
	guestFP := fsInfoToFingerprint(gInfo)
	opts.State.setEntry(it.RelPath, hostFP, guestFP)
	return nil
}

func applyPullSymlink(ctx context.Context, opts syncApplyOpts, it syncPlanItem, target string) error {
	hp, err := safeRelJoin(opts.HostRoot, it.RelPath)
	if err != nil {
		return err
	}
	gp, err := safeGuestJoin(opts.GuestRoot, it.RelPath)
	if err != nil {
		return err
	}
	gInfo, err := opts.FS.Stat(ctx, gp)
	if err != nil {
		return fmt.Errorf("stat guest %s: %w", gp, err)
	}
	if target == "" {
		target = gInfo.Target
	}
	if target == "" {
		return fmt.Errorf("pull symlink %s: empty target (agent may lack target in FSInfo)", it.RelPath)
	}
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		return err
	}
	// Replace any existing path at the destination.
	_ = os.RemoveAll(hp)
	if err := os.Symlink(target, hp); err != nil {
		return fmt.Errorf("host symlink %s: %w", it.RelPath, err)
	}
	hfi, err := os.Lstat(hp)
	if err != nil {
		return err
	}
	hostFP := lstatToFingerprint(hp, hfi)
	guestFP := fsInfoToFingerprint(gInfo)
	if guestFP != nil && guestFP.Target == "" {
		guestFP.Target = target
	}
	opts.State.setEntry(it.RelPath, hostFP, guestFP)
	return nil
}

func applyPushFile(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	hp, err := safeRelJoin(opts.HostRoot, it.RelPath)
	if err != nil {
		return err
	}
	gp, err := safeGuestJoin(opts.GuestRoot, it.RelPath)
	if err != nil {
		return err
	}
	fi, err := os.Lstat(hp)
	if err != nil {
		return fmt.Errorf("host %s: %w", it.RelPath, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("host %s: is directory", it.RelPath)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return applyPushSymlink(ctx, opts, it, "")
	}
	f, err := os.Open(hp)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	mode := fmt.Sprintf("%04o", fi.Mode().Perm())
	if it.Source != nil && it.Source.Mode != "" {
		mode = normalizeSyncMode(it.Source.Mode)
	}
	if err := opts.FS.PutFile(ctx, gp, f, fi.Size(), agent.CPOpts{Mode: mode}); err != nil {
		return fmt.Errorf("put %s: %w", gp, err)
	}
	// Host fingerprint from pre-transfer Lstat; guest from post-Put Stat.
	hostFP := lstatToFingerprint(hp, fi)
	gInfo, err := opts.FS.Stat(ctx, gp)
	if err != nil {
		return fmt.Errorf("stat guest %s: %w", gp, err)
	}
	guestFP := fsInfoToFingerprint(gInfo)
	opts.State.setEntry(it.RelPath, hostFP, guestFP)
	return nil
}

func applyPullFile(ctx context.Context, opts syncApplyOpts, it syncPlanItem) error {
	hp, err := safeRelJoin(opts.HostRoot, it.RelPath)
	if err != nil {
		return err
	}
	gp, err := safeGuestJoin(opts.GuestRoot, it.RelPath)
	if err != nil {
		return err
	}
	// Guest fingerprint before transfer.
	gInfo, err := opts.FS.Stat(ctx, gp)
	if err != nil {
		return fmt.Errorf("stat guest %s: %w", gp, err)
	}
	if gInfo.Type == "symlink" {
		return applyPullSymlink(ctx, opts, it, gInfo.Target)
	}
	guestFP := fsInfoToFingerprint(gInfo)

	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		return err
	}
	mode := parseOctalMode(gInfo.Mode, 0o644)
	if it.Source != nil && it.Source.Mode != "" {
		mode = parseOctalMode(it.Source.Mode, 0o644)
	}
	f, err := os.OpenFile(hp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	cerr := opts.FS.GetFile(ctx, gp, f)
	if closeErr := f.Close(); cerr == nil {
		cerr = closeErr
	}
	if cerr != nil {
		return fmt.Errorf("get %s: %w", gp, cerr)
	}
	_ = os.Chmod(hp, mode)

	hfi, err := os.Lstat(hp)
	if err != nil {
		return err
	}
	hostFP := lstatToFingerprint(hp, hfi)
	opts.State.setEntry(it.RelPath, hostFP, guestFP)
	return nil
}

func refreshBaselineBoth(ctx context.Context, opts syncApplyOpts, rel string, it syncPlanItem) error {
	// After dir create: fingerprint host and guest.
	switch opts.Verb {
	case syncPull:
		hp, err := safeRelJoin(opts.HostRoot, rel)
		if err != nil {
			return err
		}
		hfi, err := os.Lstat(hp)
		if err != nil {
			return err
		}
		hostFP := lstatToFingerprint(hp, hfi)
		var guestFP *syncFingerprint
		if it.Source != nil {
			guestFP = invToFingerprint(it.Source)
		} else {
			gp, err := safeGuestJoin(opts.GuestRoot, rel)
			if err != nil {
				return err
			}
			gInfo, err := opts.FS.Stat(ctx, gp)
			if err != nil {
				return err
			}
			guestFP = fsInfoToFingerprint(gInfo)
		}
		opts.State.setEntry(rel, hostFP, guestFP)
	default:
		gp, err := safeGuestJoin(opts.GuestRoot, rel)
		if err != nil {
			return err
		}
		gInfo, err := opts.FS.Stat(ctx, gp)
		if err != nil {
			// Mkdir may not be immediately stat-able on all backends; use inventory.
			if it.Source != nil {
				var hostFP *syncFingerprint
				if it.Source != nil {
					hostFP = invToFingerprint(it.Source)
				}
				opts.State.setEntry(rel, hostFP, &syncFingerprint{Type: "directory", Mode: "0755"})
				return nil
			}
			return err
		}
		guestFP := fsInfoToFingerprint(gInfo)
		var hostFP *syncFingerprint
		if it.Source != nil {
			hostFP = invToFingerprint(it.Source)
		} else {
			hp, err := safeRelJoin(opts.HostRoot, rel)
			if err != nil {
				return err
			}
			hfi, err := os.Lstat(hp)
			if err != nil {
				return err
			}
			hostFP = lstatToFingerprint(hp, hfi)
		}
		opts.State.setEntry(rel, hostFP, guestFP)
	}
	return nil
}

func lstatToFingerprint(path string, fi os.FileInfo) *syncFingerprint {
	typ := "file"
	var target string
	if fi.Mode()&os.ModeSymlink != 0 {
		typ = "symlink"
		if path != "" {
			if t, err := os.Readlink(path); err == nil {
				target = t
			}
		}
	} else if fi.IsDir() {
		typ = "directory"
	}
	return &syncFingerprint{
		Type:   typ,
		Size:   fi.Size(),
		Mtime:  fi.ModTime().Unix(),
		Mode:   fmt.Sprintf("%04o", fi.Mode().Perm()),
		Target: target,
	}
}

func fsInfoToFingerprint(info *agent.FSInfo) *syncFingerprint {
	if info == nil {
		return nil
	}
	return &syncFingerprint{
		Type:   info.Type,
		Size:   info.Size,
		Mtime:  info.Mtime,
		Mode:   normalizeSyncMode(info.Mode),
		Target: info.Target,
	}
}

func parseOctalMode(s string, def os.FileMode) os.FileMode {
	s = normalizeSyncMode(s)
	if s == "" {
		return def
	}
	v, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return def
	}
	return os.FileMode(v) & os.ModePerm
}

// discardWriter discards bytes (used in tests; hashing streams into sha256 directly).
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

var _ io.Writer = discardWriter{}

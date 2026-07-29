package cli

import (
	"fmt"
	"sort"
)

// syncVerb is push (host→guest) or pull (guest→host).
type syncVerb string

const (
	syncPush syncVerb = "push"
	syncPull syncVerb = "pull"
)

// syncAction is the planned operation for one relative path.
type syncAction string

const (
	syncActCreate     syncAction = "create"
	syncActUpdate     syncAction = "update"
	syncActUpdateMode syncAction = "update_mode"
	syncActDelete     syncAction = "delete"
	syncActSkip       syncAction = "skip"
	syncActKeptDest   syncAction = "kept_dest"
	syncActConflict   syncAction = "conflict"
	syncActReplace    syncAction = "replace" // type mismatch with --force
)

// syncInvEntry is one inventoried path on host or guest.
type syncInvEntry struct {
	Type  string // file | directory | symlink
	Size  int64
	Mtime int64  // unix seconds
	Mode  string // e.g. "0644"
}

// syncClassifyOpts controls classification (flags subset for the planner).
type syncClassifyOpts struct {
	Verb   syncVerb
	Delete bool // --delete
	Force  bool // --force: conflicts + kept_dest → update
	// Checksum is reserved for PR2+ (hash refine); planner ignores for now
	// except documenting the hook.
	Checksum bool
}

// syncPlanItem is one path in the plan.
type syncPlanItem struct {
	RelPath string
	Action  syncAction
	Reason  string
	Source  *syncInvEntry // S
	Dest    *syncInvEntry // D
	// BaselineDirty when action is skip/create/... that should write baseline
	// on cold-start skip (no transfer but new B entry).
	BaselineDirty bool
}

// syncPlan is the full classification result.
type syncPlan struct {
	Items []syncPlanItem
	// Counts by action (after force remapping).
	Created       int
	Updated       int
	UpdateMode    int
	Deleted       int
	Skipped       int
	KeptDest      int
	Conflicts     int
	SkippedLink   int
	BaselineDirty int // cold-start skip+baseline paths
}

// HasConflicts reports whether any conflict remains (blocks apply unless force already applied).
func (p *syncPlan) HasConflicts() bool {
	return p != nil && p.Conflicts > 0
}

// classifyAll builds a plan from host/guest inventories and optional baseline state.
// Keys in inventories are slash-separated relative paths (no leading ./).
// ign may be nil (match nothing).
func classifyAll(
	host, guest map[string]*syncInvEntry,
	st *syncState,
	ign *syncIgnore,
	opts syncClassifyOpts,
) *syncPlan {
	// Union of all relative paths.
	seen := make(map[string]struct{})
	for k := range host {
		seen[k] = struct{}{}
	}
	for k := range guest {
		seen[k] = struct{}{}
	}
	paths := make([]string, 0, len(seen))
	for k := range seen {
		paths = append(paths, k)
	}
	sort.Strings(paths)

	plan := &syncPlan{Items: make([]syncPlanItem, 0, len(paths))}
	for _, rel := range paths {
		item := classifyPath(rel, host[rel], guest[rel], st, ign, opts)
		plan.Items = append(plan.Items, item)
		tallyPlanItem(plan, item)
	}
	return plan
}

func tallyPlanItem(plan *syncPlan, item syncPlanItem) {
	switch item.Action {
	case syncActCreate:
		plan.Created++
	case syncActUpdate, syncActReplace:
		plan.Updated++
	case syncActUpdateMode:
		plan.UpdateMode++
		plan.Updated++ // summary "updated" includes mode-only
	case syncActDelete:
		plan.Deleted++
	case syncActSkip:
		plan.Skipped++
		if item.BaselineDirty {
			plan.BaselineDirty++
		}
	case syncActKeptDest:
		plan.KeptDest++
	case syncActConflict:
		plan.Conflicts++
	}
	if item.Action == syncActSkip && item.Reason == "symlink" {
		plan.SkippedLink++
	}
}

// classifyPath implements the normative per-path gate from the design doc.
func classifyPath(
	relPath string,
	hostE, guestE *syncInvEntry,
	st *syncState,
	ign *syncIgnore,
	opts syncClassifyOpts,
) syncPlanItem {
	item := syncPlanItem{RelPath: relPath, Source: nil, Dest: nil}

	// Map S/D by verb.
	var s, d *syncInvEntry
	switch opts.Verb {
	case syncPull:
		s, d = guestE, hostE
	default: // push
		s, d = hostE, guestE
	}
	item.Source, item.Dest = s, d

	// Ignore: never transfer; delete eligibility handled separately.
	if ign != nil && ign.Match(relPath) {
		item.Action = syncActSkip
		item.Reason = "ignored"
		return item
	}

	// Symlinks: skip (K17).
	if (s != nil && s.Type == "symlink") || (d != nil && d.Type == "symlink") {
		item.Action = syncActSkip
		item.Reason = "symlink"
		return item
	}

	base, hasB := st.entry(relPath)
	// Incomplete B → cold-start.
	if hasB && !base.complete() {
		hasB = false
	}

	if !hasB {
		return classifyColdStart(item, s, d, opts)
	}
	return classifyThreeWay(item, s, d, base, opts)
}

func classifyColdStart(item syncPlanItem, s, d *syncInvEntry, opts syncClassifyOpts) syncPlanItem {
	switch {
	case s != nil && d == nil:
		item.Action = syncActCreate
		item.Reason = "cold-start: dest missing"
	case s == nil && d != nil:
		if opts.Delete {
			item.Action = syncActDelete
			item.Reason = "cold-start: orphan dest"
		} else {
			item.Action = syncActSkip
			item.Reason = "cold-start: orphan dest (no --delete)"
		}
	case s != nil && d != nil:
		if s.Type != d.Type {
			if opts.Force {
				item.Action = syncActReplace
				item.Reason = "cold-start: type mismatch (--force)"
			} else {
				item.Action = syncActConflict
				item.Reason = "cold-start: type mismatch"
			}
			return item
		}
		// Equal types.
		if s.Type == "directory" || invSizeEqual(s, d) {
			// skip + baseline dirty (no transfer)
			item.Action = syncActSkip
			item.Reason = "cold-start: size match"
			item.BaselineDirty = true
		} else {
			item.Action = syncActUpdate
			item.Reason = "cold-start: size differ"
		}
	default:
		// both nil — should not appear in union
		item.Action = syncActSkip
		item.Reason = "empty"
	}
	return item
}

func invSizeEqual(a, b *syncInvEntry) bool {
	if a == nil || b == nil {
		return false
	}
	return a.Size == b.Size
}

func classifyThreeWay(item syncPlanItem, s, d *syncInvEntry, base syncEntry, opts syncClassifyOpts) syncPlanItem {
	// Baseline mapping by verb.
	var bSrc, bDst *syncFingerprint
	switch opts.Verb {
	case syncPull:
		bSrc, bDst = base.Guest, base.Host
	default:
		bSrc, bDst = base.Host, base.Guest
	}

	sourceChanged := sideContentChanged(s, bSrc)
	destChanged := sideContentChanged(d, bDst)

	// Orphan dest / create
	if s == nil && d != nil {
		if opts.Delete {
			item.Action = syncActDelete
			item.Reason = "source missing"
		} else {
			item.Action = syncActSkip
			item.Reason = "source missing (no --delete)"
		}
		return item
	}
	if s != nil && d == nil {
		item.Action = syncActCreate
		item.Reason = "dest missing"
		return item
	}
	if s == nil && d == nil {
		item.Action = syncActSkip
		item.Reason = "empty"
		return item
	}

	// Both present.
	if s.Type != d.Type {
		if opts.Force {
			item.Action = syncActReplace
			item.Reason = "type mismatch (--force)"
		} else {
			item.Action = syncActConflict
			item.Reason = "type mismatch"
		}
		return item
	}

	// Steady-state idle: both match baseline — do NOT require equalContent(S,D).
	if !sourceChanged && !destChanged {
		// Mode-only update when source mode diverged from dest or baseline source mode.
		if s.Type == "file" {
			srcMode := normalizeSyncMode(s.Mode)
			dstMode := normalizeSyncMode(d.Mode)
			bMode := ""
			if bSrc != nil {
				bMode = normalizeSyncMode(bSrc.Mode)
			}
			if !modesEqual(srcMode, dstMode) || (bMode != "" && !modesEqual(srcMode, bMode)) {
				item.Action = syncActUpdateMode
				item.Reason = "mode only"
				return item
			}
		}
		item.Action = syncActSkip
		item.Reason = "unchanged"
		return item
	}

	if sourceChanged && destChanged {
		if opts.Force {
			item.Action = syncActUpdate
			item.Reason = "both changed (--force source wins)"
		} else {
			item.Action = syncActConflict
			item.Reason = "both changed"
		}
		return item
	}
	if sourceChanged && !destChanged {
		item.Action = syncActUpdate
		item.Reason = "source changed"
		return item
	}
	// !sourceChanged && destChanged → kept_dest (K11) unless --force
	if opts.Force {
		item.Action = syncActUpdate
		item.Reason = "dest ahead (--force source wins)"
	} else {
		item.Action = syncActKeptDest
		item.Reason = "dest ahead"
	}
	return item
}

// sideContentChanged reports whether live inventory diverged from baseline content fp.
func sideContentChanged(live *syncInvEntry, base *syncFingerprint) bool {
	if live == nil {
		return base != nil // was baselined, now missing
	}
	if base == nil {
		return true // live present without baseline side (treat as changed)
	}
	return !fpContentEqual(invToFingerprint(live), base)
}

// planSummaryLine is a one-line human summary (tests / future CLI).
func planSummaryLine(p *syncPlan) string {
	if p == nil {
		return "empty plan"
	}
	return fmt.Sprintf(
		"created=%d updated=%d deleted=%d skipped=%d kept_dest=%d conflicts=%d baseline_dirty=%d",
		p.Created, p.Updated, p.Deleted, p.Skipped, p.KeptDest, p.Conflicts, p.BaselineDirty,
	)
}

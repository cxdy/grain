package vmsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// refinePlanChecksum upgrades skip decisions to update when host and guest file
// content hashes differ. Invoked only when Options.Checksum is true, after the
// size/mtime planner. Directories, symlinks, ignores, and one-sided paths are left alone.
func refinePlanChecksum(ctx context.Context, plan *syncPlan, opts Options, hostRoot, guestRoot string) error {
	if plan == nil || !opts.Checksum {
		return nil
	}
	changed := false
	for i := range plan.Items {
		it := &plan.Items[i]
		if it.Action != syncActSkip {
			continue
		}
		// Content refine only applies when both sides exist as regular files and
		// the size/mtime planner would have skipped a transfer.
		if it.Source == nil || it.Dest == nil {
			continue
		}
		if it.Source.Type != "file" || it.Dest.Type != "file" {
			continue
		}
		switch it.Reason {
		case "ignored", "symlink", "empty":
			continue
		}

		hostSum, err := hashHostFile(hostRoot, it.RelPath)
		if err != nil {
			return fmt.Errorf("sync: checksum host %s: %w", it.RelPath, err)
		}
		guestSum, err := hashGuestFile(ctx, opts.FS, guestRoot, it.RelPath)
		if err != nil {
			return fmt.Errorf("sync: checksum guest %s: %w", it.RelPath, err)
		}
		if hostSum == guestSum {
			// Content matches; keep skip (optionally annotate reason).
			if it.Reason == "cold-start: size match" || it.Reason == "unchanged" {
				it.Reason = "checksum match"
			}
			continue
		}
		// Content differs: transfer source → dest regardless of size/mtime equality.
		it.Action = syncActUpdate
		it.Reason = "checksum differ"
		it.BaselineDirty = false
		changed = true
	}
	if changed {
		retallyPlan(plan)
	}
	return nil
}

// retallyPlan recomputes summary counters from Items (after refine mutates actions).
func retallyPlan(p *syncPlan) {
	if p == nil {
		return
	}
	p.Created = 0
	p.Updated = 0
	p.UpdateMode = 0
	p.Deleted = 0
	p.Skipped = 0
	p.KeptDest = 0
	p.Conflicts = 0
	p.SkippedLink = 0
	p.BaselineDirty = 0
	for _, it := range p.Items {
		tallyPlanItem(p, it)
	}
}

// hashHostFile returns the hex-encoded SHA-256 of the host file at root/rel.
func hashHostFile(hostRoot, rel string) (string, error) {
	p, err := safeRelJoin(hostRoot, rel)
	if err != nil {
		return "", err
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashGuestFile streams the guest file via GetFile into a SHA-256 hasher.
func hashGuestFile(ctx context.Context, gfs FS, guestRoot, rel string) (string, error) {
	if gfs == nil {
		return "", fmt.Errorf("guest FS required")
	}
	gp, err := safeGuestJoin(guestRoot, rel)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if err := gfs.GetFile(ctx, gp, h); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

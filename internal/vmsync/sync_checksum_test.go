package vmsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefinePlanChecksumDisabledAndNil(t *testing.T) {
	t.Parallel()
	if err := refinePlanChecksum(context.Background(), nil, Options{Checksum: true}, "", ""); err != nil {
		t.Fatal(err)
	}
	plan := &syncPlan{Items: []syncPlanItem{{RelPath: "a", Action: syncActSkip}}}
	if err := refinePlanChecksum(context.Background(), plan, Options{Checksum: false}, t.TempDir(), "/work"); err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].Action != syncActSkip {
		t.Fatal("disabled must not change")
	}
}

func TestRefinePlanChecksumMatchAndDiffer(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	body := []byte("same-content")
	if err := os.WriteFile(filepath.Join(host, "f.txt"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])

	fs := newMemGuestFS()
	fs.files["/work/f.txt"] = append([]byte(nil), body...)
	fs.files["/work/diff.txt"] = []byte("guest-different")
	if err := os.WriteFile(filepath.Join(host, "diff.txt"), []byte("host-different"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := &syncPlan{
		Items: []syncPlanItem{
			{
				RelPath: "f.txt", Action: syncActSkip, Reason: "unchanged",
				Source: &syncInvEntry{Type: "file", Size: int64(len(body)), Mode: "0644"},
				Dest:   &syncInvEntry{Type: "file", Size: int64(len(body)), Mode: "0644"},
			},
			{
				RelPath: "diff.txt", Action: syncActSkip, Reason: "cold-start: size match",
				Source: &syncInvEntry{Type: "file", Size: 14, Mode: "0644"},
				Dest:   &syncInvEntry{Type: "file", Size: 15, Mode: "0644"},
			},
			// non-skip: ignored
			{RelPath: "x", Action: syncActCreate, Source: &syncInvEntry{Type: "file"}},
			// missing source/dest
			{RelPath: "one", Action: syncActSkip, Source: &syncInvEntry{Type: "file"}},
			// directories
			{
				RelPath: "d", Action: syncActSkip,
				Source: &syncInvEntry{Type: "directory"}, Dest: &syncInvEntry{Type: "directory"},
			},
			// ignored reason
			{
				RelPath: "ig", Action: syncActSkip, Reason: "ignored",
				Source: &syncInvEntry{Type: "file"}, Dest: &syncInvEntry{Type: "file"},
			},
			// symlink reason
			{
				RelPath: "l", Action: syncActSkip, Reason: "symlink",
				Source: &syncInvEntry{Type: "file"}, Dest: &syncInvEntry{Type: "file"},
			},
			// empty reason
			{
				RelPath: "e", Action: syncActSkip, Reason: "empty",
				Source: &syncInvEntry{Type: "file"}, Dest: &syncInvEntry{Type: "file"},
			},
		},
		Skipped: 7, Created: 1,
	}

	err := refinePlanChecksum(context.Background(), plan, Options{Checksum: true, FS: fs}, host, "/work")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].Action != syncActSkip || plan.Items[0].Reason != "checksum match" {
		t.Fatalf("match: %+v", plan.Items[0])
	}
	if plan.Items[1].Action != syncActUpdate || plan.Items[1].Reason != "checksum differ" {
		t.Fatalf("differ: %+v", plan.Items[1])
	}
	if plan.Items[1].BaselineDirty {
		t.Fatal("baseline dirty cleared")
	}
	// retally should have moved counts
	if plan.Updated < 1 || plan.Skipped < 1 {
		t.Fatalf("retally updated=%d skipped=%d", plan.Updated, plan.Skipped)
	}
	_ = hexSum
}

func TestRefinePlanChecksumErrors(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	// host file missing
	plan := &syncPlan{Items: []syncPlanItem{{
		RelPath: "missing.txt", Action: syncActSkip, Reason: "unchanged",
		Source: &syncInvEntry{Type: "file", Size: 1}, Dest: &syncInvEntry{Type: "file", Size: 1},
	}}}
	err := refinePlanChecksum(context.Background(), plan, Options{Checksum: true, FS: fs}, host, "/work")
	if err == nil || !strings.Contains(err.Error(), "checksum host") {
		t.Fatalf("host err: %v", err)
	}

	// guest missing
	if err := os.WriteFile(filepath.Join(host, "g.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan2 := &syncPlan{Items: []syncPlanItem{{
		RelPath: "g.txt", Action: syncActSkip, Reason: "unchanged",
		Source: &syncInvEntry{Type: "file", Size: 1}, Dest: &syncInvEntry{Type: "file", Size: 1},
	}}}
	err = refinePlanChecksum(context.Background(), plan2, Options{Checksum: true, FS: fs}, host, "/work")
	if err == nil || !strings.Contains(err.Error(), "checksum guest") {
		t.Fatalf("guest err: %v", err)
	}

	// nil FS
	err = refinePlanChecksum(context.Background(), plan2, Options{Checksum: true, FS: nil}, host, "/work")
	if err == nil {
		t.Fatal("nil fs")
	}
}

func TestHashHostAndGuest(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	body := []byte("hash-me")
	if err := os.WriteFile(filepath.Join(host, "h.txt"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := hashHostFile(host, "h.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(body)
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("%s", sum)
	}
	if _, err := hashHostFile(host, "../escape"); err == nil {
		t.Fatal("escape")
	}
	if _, err := hashHostFile(host, "nope"); err == nil {
		t.Fatal("missing")
	}

	fs := newMemGuestFS()
	fs.files["/work/h.txt"] = body
	gsum, err := hashGuestFile(context.Background(), fs, "/work", "h.txt")
	if err != nil {
		t.Fatal(err)
	}
	if gsum != sum {
		t.Fatalf("guest %s host %s", gsum, sum)
	}
	if _, err := hashGuestFile(context.Background(), nil, "/work", "h.txt"); err == nil {
		t.Fatal("nil fs")
	}
	if _, err := hashGuestFile(context.Background(), fs, "/work", "../x"); err == nil {
		t.Fatal("escape guest")
	}
}

func TestRetallyPlan(t *testing.T) {
	t.Parallel()
	retallyPlan(nil)
	p := &syncPlan{Items: []syncPlanItem{
		{Action: syncActCreate},
		{Action: syncActUpdate},
		{Action: syncActUpdateMode},
		{Action: syncActDelete},
		{Action: syncActSkip, BaselineDirty: true},
		{Action: syncActSkip, Reason: "symlink"},
		{Action: syncActKeptDest},
		{Action: syncActConflict},
		{Action: syncActReplace},
	}}
	retallyPlan(p)
	if p.Created != 1 || p.Updated != 3 || p.Deleted != 1 || p.Skipped != 2 || p.KeptDest != 1 || p.Conflicts != 1 {
		t.Fatalf("%+v", p)
	}
	if p.SkippedLink != 1 || p.BaselineDirty != 1 || p.UpdateMode != 1 {
		t.Fatalf("link/mode %+v", p)
	}
}

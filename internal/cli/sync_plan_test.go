package cli

import (
	"testing"
)

func inv(size, mtime int64, mode string) *syncInvEntry {
	return &syncInvEntry{Type: "file", Size: size, Mtime: mtime, Mode: mode}
}

func baseState(rel string, host, guest *syncFingerprint) *syncState {
	st := newSyncState("local:s", "vm", "/h", "/g")
	st.setEntry(rel, host, guest)
	return st
}

func TestClassifyBaselineSkipDifferentMtimes(t *testing.T) {
	t.Parallel()
	// After push: host mtime 1000, guest mtime 2000 (Put rewrote); both match B.
	st := baseState("a.go",
		&syncFingerprint{Type: "file", Size: 10, Mtime: 1000, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 10, Mtime: 2000, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"a.go": inv(10, 1000, "0644")}
	guest := map[string]*syncInvEntry{"a.go": inv(10, 2000, "0644")}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush})
	if len(plan.Items) != 1 || plan.Items[0].Action != syncActSkip {
		t.Fatalf("want skip, got %+v", plan.Items)
	}
	if plan.Conflicts != 0 {
		t.Fatal("must not drift-conflict")
	}
}

func TestClassifyPushThenPullNoEditsAllSkip(t *testing.T) {
	t.Parallel()
	st := baseState("f",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 10, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 99, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"f": inv(1, 10, "0644")}
	guest := map[string]*syncInvEntry{"f": inv(1, 99, "0644")}
	for _, verb := range []syncVerb{syncPush, syncPull} {
		plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: verb})
		if plan.Skipped != 1 || plan.Conflicts != 0 {
			t.Fatalf("%s: %s", verb, planSummaryLine(plan))
		}
	}
}

func TestClassifyHostOnlyEditPushUpdate(t *testing.T) {
	t.Parallel()
	st := baseState("f",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 10, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 20, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"f": inv(2, 11, "0644")} // content changed
	guest := map[string]*syncInvEntry{"f": inv(1, 20, "0644")}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush})
	if plan.Items[0].Action != syncActUpdate {
		t.Fatalf("got %s", plan.Items[0].Action)
	}
}

func TestClassifyGuestOnlyEditPushKeptDest(t *testing.T) {
	t.Parallel()
	st := baseState("f",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 10, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 20, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"f": inv(1, 10, "0644")}
	guest := map[string]*syncInvEntry{"f": inv(9, 30, "0644")} // guest only
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush})
	if plan.Items[0].Action != syncActKeptDest {
		t.Fatalf("got %s (%s)", plan.Items[0].Action, plan.Items[0].Reason)
	}
}

func TestClassifyGuestOnlyEditPullUpdate(t *testing.T) {
	t.Parallel()
	st := baseState("f",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 10, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 20, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"f": inv(1, 10, "0644")}
	guest := map[string]*syncInvEntry{"f": inv(9, 30, "0644")}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPull})
	// pull: S=guest (changed), D=host (unchanged) → update
	if plan.Items[0].Action != syncActUpdate {
		t.Fatalf("got %s (%s)", plan.Items[0].Action, plan.Items[0].Reason)
	}
}

func TestClassifyBothChangedConflict(t *testing.T) {
	t.Parallel()
	st := baseState("f",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 10, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 20, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"f": inv(2, 11, "0644")}
	guest := map[string]*syncInvEntry{"f": inv(3, 21, "0644")}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush})
	if plan.Conflicts != 1 || plan.Items[0].Action != syncActConflict {
		t.Fatalf("got %s", planSummaryLine(plan))
	}
	// force → update
	plan2 := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush, Force: true})
	if plan2.Items[0].Action != syncActUpdate {
		t.Fatalf("force: %s", plan2.Items[0].Action)
	}
}

func TestClassifyColdStartEqualSize(t *testing.T) {
	t.Parallel()
	host := map[string]*syncInvEntry{"f": inv(5, 1, "0644")}
	guest := map[string]*syncInvEntry{"f": inv(5, 99, "0644")}
	plan := classifyAll(host, guest, nil, nil, syncClassifyOpts{Verb: syncPush})
	if plan.Items[0].Action != syncActSkip || !plan.Items[0].BaselineDirty {
		t.Fatalf("got %+v", plan.Items[0])
	}
	if plan.BaselineDirty != 1 {
		t.Fatalf("baseline_dirty=%d", plan.BaselineDirty)
	}
}

func TestClassifyColdStartUnequalSize(t *testing.T) {
	t.Parallel()
	host := map[string]*syncInvEntry{"f": inv(5, 1, "0644")}
	guest := map[string]*syncInvEntry{"f": inv(6, 99, "0644")}
	plan := classifyAll(host, guest, nil, nil, syncClassifyOpts{Verb: syncPush})
	if plan.Items[0].Action != syncActUpdate {
		t.Fatalf("got %s", plan.Items[0].Action)
	}
}

func TestClassifyNewPathWhileStateExists(t *testing.T) {
	t.Parallel()
	st := baseState("old",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 2, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{
		"old": inv(1, 1, "0644"),
		"new": inv(8, 3, "0644"),
	}
	guest := map[string]*syncInvEntry{
		"old": inv(1, 2, "0644"),
		"new": inv(8, 4, "0644"), // same size → cold-start skip+baseline
	}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush})
	by := map[string]syncPlanItem{}
	for _, it := range plan.Items {
		by[it.RelPath] = it
	}
	if by["old"].Action != syncActSkip || by["old"].BaselineDirty {
		t.Fatalf("old: %+v", by["old"])
	}
	if by["new"].Action != syncActSkip || !by["new"].BaselineDirty {
		t.Fatalf("new: %+v", by["new"])
	}
	// different size
	guest["new"] = inv(9, 4, "0644")
	plan2 := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush})
	for _, it := range plan2.Items {
		if it.RelPath == "new" && it.Action != syncActUpdate {
			t.Fatalf("new size differ: %s", it.Action)
		}
	}
}

func TestClassifyModeOnly(t *testing.T) {
	t.Parallel()
	st := baseState("f",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 10, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 20, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"f": inv(1, 10, "0755")} // mode only
	guest := map[string]*syncInvEntry{"f": inv(1, 20, "0644")}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush})
	if plan.Items[0].Action != syncActUpdateMode {
		t.Fatalf("got %s (%s)", plan.Items[0].Action, plan.Items[0].Reason)
	}
}

func TestClassifyForceDestAhead(t *testing.T) {
	t.Parallel()
	st := baseState("f",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 10, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 20, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{"f": inv(1, 10, "0644")}
	guest := map[string]*syncInvEntry{"f": inv(9, 30, "0644")}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush, Force: true})
	if plan.Items[0].Action != syncActUpdate {
		t.Fatalf("got %s", plan.Items[0].Action)
	}
}

func TestClassifyCreateAndDelete(t *testing.T) {
	t.Parallel()
	st := baseState("keep",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 2, Mode: "0644"},
	)
	host := map[string]*syncInvEntry{
		"keep": inv(1, 1, "0644"),
		"new":  inv(3, 5, "0644"),
	}
	guest := map[string]*syncInvEntry{
		"keep": inv(1, 2, "0644"),
		"old":  inv(1, 2, "0644"), // dest only
	}
	plan := classifyAll(host, guest, st, nil, syncClassifyOpts{Verb: syncPush, Delete: true})
	by := map[string]syncAction{}
	for _, it := range plan.Items {
		by[it.RelPath] = it.Action
	}
	if by["new"] != syncActCreate {
		t.Fatalf("new=%s", by["new"])
	}
	if by["old"] != syncActDelete {
		t.Fatalf("old=%s", by["old"])
	}
}

func TestClassifyIgnored(t *testing.T) {
	t.Parallel()
	ign, err := buildSyncIgnore(syncIgnoreOpts{NoDefaults: true, ExtraLines: []string{"skipme"}})
	if err != nil {
		t.Fatal(err)
	}
	host := map[string]*syncInvEntry{"skipme": inv(1, 1, "0644"), "ok": inv(1, 1, "0644")}
	plan := classifyAll(host, nil, nil, ign, syncClassifyOpts{Verb: syncPush})
	by := map[string]syncPlanItem{}
	for _, it := range plan.Items {
		by[it.RelPath] = it
	}
	if by["skipme"].Action != syncActSkip || by["skipme"].Reason != "ignored" {
		t.Fatalf("%+v", by["skipme"])
	}
	if by["ok"].Action != syncActCreate {
		t.Fatalf("%+v", by["ok"])
	}
}

func TestClassifySymlinkSkip(t *testing.T) {
	t.Parallel()
	host := map[string]*syncInvEntry{"l": {Type: "symlink", Size: 0, Mtime: 1}}
	plan := classifyAll(host, nil, nil, nil, syncClassifyOpts{Verb: syncPush})
	if plan.Items[0].Action != syncActSkip || plan.SkippedLink != 1 {
		t.Fatalf("%+v counts link=%d", plan.Items[0], plan.SkippedLink)
	}
}

func TestClassifyDeleteEligibleViaIgnore(t *testing.T) {
	t.Parallel()
	// dest-only ignored path: classify as skip(ignored) before delete logic
	ign, _ := buildSyncIgnore(syncIgnoreOpts{NoDefaults: true, ExtraLines: []string{"build/"}})
	guest := map[string]*syncInvEntry{"build/out": inv(1, 1, "0644")}
	plan := classifyAll(nil, guest, nil, ign, syncClassifyOpts{Verb: syncPush, Delete: true})
	if plan.Items[0].Action != syncActSkip {
		t.Fatalf("ignored orphan should skip not delete: %s", plan.Items[0].Action)
	}
}

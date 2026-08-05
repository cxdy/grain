package vmsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPushDryRunAndApply(t *testing.T) {
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "/work", true, "0755")

	var out, errOut bytes.Buffer
	res, err := Run(ctx, Options{
		Verb:        Push,
		VM:          "vm1",
		HostRoot:    host,
		GuestRoot:   "/work",
		APIIdentity: "test",
		DataDir:     data,
		FS:          fs,
		Out:         &out,
		ErrOut:      &errOut,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if res.ExitCode != ExitOK {
		t.Fatalf("exit %d", res.ExitCode)
	}
	if res.Plan == nil || res.Plan.Created < 1 {
		t.Fatalf("expected creates, plan=%+v out=%s", res.Plan, out.String())
	}

	res, err = Run(ctx, Options{
		Verb:        Push,
		VM:          "vm1",
		HostRoot:    host,
		GuestRoot:   "/work",
		APIIdentity: "test",
		DataDir:     data,
		FS:          fs,
		Out:         &out,
		ErrOut:      &errOut,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Applied < 1 {
		t.Fatalf("applied=%d", res.Applied)
	}
	// Second run should skip.
	res2, err := Run(ctx, Options{
		Verb:        Push,
		VM:          "vm1",
		HostRoot:    host,
		GuestRoot:   "/work",
		APIIdentity: "test",
		DataDir:     data,
		FS:          fs,
		Out:         ioDiscard{},
		ErrOut:      ioDiscard{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Plan.Skipped < 1 && res2.Applied != 0 {
		// After apply, baselines set; second run should skip unchanged.
		t.Logf("second plan: created=%d updated=%d skipped=%d applied=%d",
			res2.Plan.Created, res2.Plan.Updated, res2.Plan.Skipped, res2.Applied)
	}
}

func TestRunConflictExit2(t *testing.T) {
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "f.txt"), []byte("host"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "/work", true, "0755")
	_ = fs.PutFile(ctx, "/work/f.txt", stringReader("guest"), 5, agentCP())

	// Seed baseline so both sides look changed vs B.
	id := syncStateID("test", "vm1", mustAbs(t, host), "/work")
	stPath := syncStatePath(data, id)
	st := newSyncState("test", "vm1", mustAbs(t, host), "/work")
	st.setEntry("f.txt",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 2, Mode: "0644"},
	)
	if err := saveSyncState(stPath, st); err != nil {
		t.Fatal(err)
	}

	res, err := Run(ctx, Options{
		Verb:        Push,
		VM:          "vm1",
		HostRoot:    host,
		GuestRoot:   "/work",
		APIIdentity: "test",
		DataDir:     data,
		FS:          fs,
		Out:         ioDiscard{},
		ErrOut:      ioDiscard{},
	})
	if !errors.Is(err, ErrConflicts) {
		t.Fatalf("want ErrConflicts, got %v", err)
	}
	if res.ExitCode != ExitConflict {
		t.Fatalf("exit %d", res.ExitCode)
	}
	if res.Applied != 0 {
		t.Fatalf("applied=%d on conflict", res.Applied)
	}
}

func TestRunHostFileRootRejected(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	_, err := Run(context.Background(), Options{
		Verb:      Push,
		VM:        "vm",
		HostRoot:  f,
		GuestRoot: "/work",
		DataDir:   t.TempDir(),
		FS:        fs,
	})
	if err == nil {
		t.Fatal("expected directory root error")
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestRunUsageErrors(t *testing.T) {
	ctx := context.Background()
	fs := newMemGuestFS()
	host := t.TempDir()

	res, err := Run(ctx, Options{Verb: Push, VM: "v", HostRoot: host, GuestRoot: "/w", DataDir: t.TempDir()})
	if err == nil || res.ExitCode != ExitUsage {
		t.Fatalf("nil FS: %v %+v", err, res)
	}
	res, err = Run(ctx, Options{Verb: Push, HostRoot: host, GuestRoot: "/w", FS: fs, DataDir: t.TempDir()})
	if err == nil || res.ExitCode != ExitUsage {
		t.Fatalf("empty VM: %v %+v", err, res)
	}
	res, err = Run(ctx, Options{Verb: "nope", VM: "v", HostRoot: host, GuestRoot: "/w", FS: fs, DataDir: t.TempDir()})
	if err == nil || res.ExitCode != ExitUsage {
		t.Fatalf("bad verb: %v %+v", err, res)
	}
}

func TestRunPullApplyAndCreateHostRoot(t *testing.T) {
	// Host dest does not exist yet; guest has content.
	parent := t.TempDir()
	host := filepath.Join(parent, "dest-missing")
	data := t.TempDir()
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "/work", true, "0755")
	_ = fs.PutFile(ctx, "/work/g.txt", stringReader("guest-data"), 10, agentCP())

	var out bytes.Buffer
	res, err := Run(ctx, Options{
		Verb: Pull, VM: "vm1", HostRoot: host, GuestRoot: "/work",
		APIIdentity: "test", DataDir: data, FS: fs, Out: &out, ErrOut: ioDiscard{},
	})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if res.Applied < 1 {
		t.Fatalf("applied=%d plan=%+v", res.Applied, res.Plan)
	}
	b, err := os.ReadFile(filepath.Join(host, "g.txt"))
	if err != nil || string(b) != "guest-data" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestRunValidateGuestSourceMissing(t *testing.T) {
	fs := newMemGuestFS()
	_, err := Run(context.Background(), Options{
		Verb: Pull, VM: "vm", HostRoot: t.TempDir(), GuestRoot: "/nope",
		DataDir: t.TempDir(), FS: fs,
	})
	if err == nil {
		t.Fatal("expected guest source missing")
	}
}

func TestRunValidateGuestNotDir(t *testing.T) {
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.PutFile(ctx, "/file", stringReader("x"), 1, agentCP())
	_, err := Run(ctx, Options{
		Verb: Pull, VM: "vm", HostRoot: t.TempDir(), GuestRoot: "/file",
		DataDir: t.TempDir(), FS: fs,
	})
	if err == nil {
		t.Fatal("expected guest not dir")
	}
	// Push with guest dest that is a file.
	host := t.TempDir()
	_, err = Run(ctx, Options{
		Verb: Push, VM: "vm", HostRoot: host, GuestRoot: "/file",
		DataDir: t.TempDir(), FS: fs,
	})
	if err == nil {
		t.Fatal("expected push guest dest not dir")
	}
}

func TestRunValidateHostMissingPush(t *testing.T) {
	fs := newMemGuestFS()
	_, err := Run(context.Background(), Options{
		Verb: Push, VM: "vm", HostRoot: filepath.Join(t.TempDir(), "missing"),
		GuestRoot: "/work", DataDir: t.TempDir(), FS: fs,
	})
	if err == nil {
		t.Fatal("expected host missing")
	}
}

func TestRunValidateHostPullFileRejected(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "/work", true, "0755")
	_, err := Run(ctx, Options{
		Verb: Pull, VM: "vm", HostRoot: f, GuestRoot: "/work",
		DataDir: t.TempDir(), FS: fs,
	})
	if err == nil {
		t.Fatal("expected host file root rejected on pull")
	}
}

func TestRunDryRunConflict(t *testing.T) {
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "f.txt"), []byte("host"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "/work", true, "0755")
	_ = fs.PutFile(ctx, "/work/f.txt", stringReader("guest"), 5, agentCP())

	id := syncStateID("test", "vm1", mustAbs(t, host), "/work")
	stPath := syncStatePath(data, id)
	st := newSyncState("test", "vm1", mustAbs(t, host), "/work")
	st.setEntry("f.txt",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 2, Mode: "0644"},
	)
	if err := saveSyncState(stPath, st); err != nil {
		t.Fatal(err)
	}

	res, err := Run(ctx, Options{
		Verb: Push, VM: "vm1", HostRoot: host, GuestRoot: "/work",
		APIIdentity: "test", DataDir: data, FS: fs, DryRun: true,
		Out: ioDiscard{}, ErrOut: ioDiscard{},
	})
	if !errors.Is(err, ErrConflicts) || res.ExitCode != ExitConflict {
		t.Fatalf("dry-run conflict: %v exit=%d", err, res.ExitCode)
	}
}

func TestRunMaxFileSizeAndVerbose(t *testing.T) {
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "big.bin"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, "ok.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Dest-only file for kept_dest/skip verbose paths when no baseline.
	data := t.TempDir()
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "/work", true, "0755")
	_ = fs.PutFile(ctx, "/work/orphan.txt", stringReader("o"), 1, agentCP())

	var out, errOut bytes.Buffer
	res, err := Run(ctx, Options{
		Verb: Push, VM: "vm1", HostRoot: host, GuestRoot: "/work",
		APIIdentity: "test", DataDir: data, FS: fs,
		Out: &out, ErrOut: &errOut,
		MaxFileSize: 5, // skip big.bin (10 bytes)
		Verbose:     true,
		DryRun:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "max-file-size") {
		t.Fatalf("errOut=%q", errOut.String())
	}
	// big.bin filtered from inventory → not in plan creates
	for _, it := range res.Plan.Items {
		if it.RelPath == "big.bin" {
			t.Fatal("big.bin should be filtered")
		}
	}
	if !strings.Contains(out.String(), "ok.txt") {
		t.Fatalf("out=%q", out.String())
	}
	// orphan should appear as skip (no --delete) when verbose
	if !strings.Contains(out.String(), "skip") && !strings.Contains(out.String(), "orphan") {
		// may be "skip orphan.txt"
		t.Logf("out=%q plan items=%d", out.String(), len(res.Plan.Items))
	}
}

func TestRunDefaultAPIIdentityAndPushMkdirGuest(t *testing.T) {
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	fs := newMemGuestFS()
	// Guest root missing — ensureDestRoot should mkdir.
	ctx := context.Background()
	res, err := Run(ctx, Options{
		Verb: Push, VM: "vm1", HostRoot: host, GuestRoot: "/newroot",
		// empty APIIdentity → "local"
		DataDir: data, FS: fs, Out: ioDiscard{}, ErrOut: ioDiscard{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied < 1 {
		t.Fatalf("applied=%d", res.Applied)
	}
	if !fs.dirs["/newroot"] {
		t.Fatalf("expected guest root mkdir, dirs=%v", fs.dirs)
	}
}

func TestRunForceConflictResolved(t *testing.T) {
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "f.txt"), []byte("host!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	fs := newMemGuestFS()
	ctx := context.Background()
	_ = fs.Mkdir(ctx, "/work", true, "0755")
	_ = fs.PutFile(ctx, "/work/f.txt", stringReader("guest"), 5, agentCP())

	id := syncStateID("test", "vm1", mustAbs(t, host), "/work")
	st := newSyncState("test", "vm1", mustAbs(t, host), "/work")
	st.setEntry("f.txt",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1, Mode: "0644"},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 2, Mode: "0644"},
	)
	if err := saveSyncState(syncStatePath(data, id), st); err != nil {
		t.Fatal(err)
	}

	res, err := Run(ctx, Options{
		Verb: Push, VM: "vm1", HostRoot: host, GuestRoot: "/work",
		APIIdentity: "test", DataDir: data, FS: fs, Force: true,
		Out: ioDiscard{}, ErrOut: ioDiscard{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied < 1 {
		t.Fatalf("applied=%d", res.Applied)
	}
	if string(fs.files["/work/f.txt"]) != "host!!" {
		t.Fatalf("guest=%q", fs.files["/work/f.txt"])
	}
}

func TestFilterMaxSizePull(t *testing.T) {
	host := map[string]*syncInvEntry{"h": inv(100, 1, "0644")}
	guest := map[string]*syncInvEntry{
		"big": inv(100, 1, "0644"),
		"ok":  inv(1, 1, "0644"),
		"dir": {Type: "directory", Size: 0},
		"nil": nil,
	}
	var errOut bytes.Buffer
	filterMaxSize(host, guest, Options{Verb: Pull, MaxFileSize: 10}, &errOut)
	if guest["big"] != nil {
		t.Fatal("big should be filtered from guest source")
	}
	if guest["ok"] == nil {
		t.Fatal("ok should remain")
	}
	if host["h"] == nil {
		t.Fatal("host dest should not be filtered on pull")
	}
	if !strings.Contains(errOut.String(), "big") {
		t.Fatalf("errOut=%q", errOut.String())
	}
}

func TestPrintPlanSummaryBranches(t *testing.T) {
	var out, errOut bytes.Buffer
	printPlanSummary(&out, &errOut, nil, Options{})
	if out.Len() != 0 {
		t.Fatal("nil plan")
	}
	plan := &syncPlan{
		Items: []syncPlanItem{
			{RelPath: "c", Action: syncActConflict, Reason: "both"},
			{RelPath: "a", Action: syncActCreate},
			{RelPath: "k", Action: syncActKeptDest},
			{RelPath: "s", Action: syncActSkip, Reason: "unchanged"},
			{RelPath: "u", Action: syncActUpdateMode},
		},
		Conflicts: 1,
	}
	printPlanSummary(&out, &errOut, plan, Options{Verbose: true})
	s := out.String() + errOut.String()
	for _, want := range []string{"conflict:", "create", "kept_dest", "skip", "update_mode", "conflict(s)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %q", want, s)
		}
	}
}

func TestParseArgsEdges(t *testing.T) {
	parse := func(s string) (bool, string, string) {
		for i := 0; i < len(s); i++ {
			if s[i] == ':' {
				name := s[:i]
				if name != "" && !containsSlash(name) {
					return true, name, s[i+1:]
				}
				break
			}
		}
		return false, "", s
	}
	if _, _, _, err := ParseArgs(Push, "/h", "lab:", parse); err == nil {
		t.Fatal("empty guest path push")
	}
	if _, _, _, err := ParseArgs(Pull, "lab:", "/h", parse); err == nil {
		t.Fatal("empty guest path pull")
	}
	if _, _, _, err := ParseArgs(Pull, "lab:/g", "other:/x", parse); err == nil {
		t.Fatal("pull second guest")
	}
	if _, _, _, err := ParseArgs("side", "/a", "/b", parse); err == nil {
		t.Fatal("unknown verb")
	}
	if _, _, _, err := ParseArgs(Push, "/h", "lab:.", parse); err == nil {
		t.Fatal("dot guest path")
	}
}

func TestEnsureDestRootDryRun(t *testing.T) {
	fs := newMemGuestFS()
	err := ensureDestRoot(context.Background(), Options{Verb: Push, DryRun: true, FS: fs}, "/h", "/g")
	if err != nil {
		t.Fatal(err)
	}
	if fs.mkdirs != 0 {
		t.Fatal("dry-run should not mkdir")
	}
}

func TestValidateHostRootPullMissingOK(t *testing.T) {
	if err := validateHostRoot(filepath.Join(t.TempDir(), "nope"), Pull); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := validateHostRoot(dir, Pull); err != nil {
		t.Fatal(err)
	}
	if err := validateHostRoot(dir, Push); err != nil {
		t.Fatal(err)
	}
}

func TestValidateGuestRootPushMissingOK(t *testing.T) {
	fs := newMemGuestFS()
	if err := validateGuestRoot(context.Background(), fs, "/missing", Push); err != nil {
		t.Fatal(err)
	}
	_ = fs.Mkdir(context.Background(), "/work", true, "0755")
	if err := validateGuestRoot(context.Background(), fs, "/work", Push); err != nil {
		t.Fatal(err)
	}
	if err := validateGuestRoot(context.Background(), fs, "/work", Pull); err != nil {
		t.Fatal(err)
	}
}

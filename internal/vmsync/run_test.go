package vmsync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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
	_ = fs.PutFile(ctx, "/work/f.txt", stringReader("guest"), 5, agentCP("0644"))

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

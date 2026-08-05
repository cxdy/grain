package vmsync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cxdy/grain/internal/agent"
)

// memGuestFS is an in-memory syncFS for apply tests.
type memGuestFS struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
	// failPutPath triggers error on PutFile for that path
	failPutPath string
	puts        int
	gets        int
	rms         int
	mkdirs      int
}

func newMemGuestFS() *memGuestFS {
	return &memGuestFS{
		files: map[string][]byte{},
		dirs:  map[string]bool{"/": true},
	}
}

func (m *memGuestFS) Stat(_ context.Context, path string) (*agent.FSInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.files[path]; ok {
		return &agent.FSInfo{Name: path, Type: "file", Size: int64(len(b)), Mode: "0644", Mtime: 100}, nil
	}
	if m.dirs[path] {
		return &agent.FSInfo{Name: path, Type: "directory", Mode: "0755", Mtime: 100}, nil
	}
	return nil, fmt.Errorf("stat: not found")
}

func (m *memGuestFS) ReadDir(_ context.Context, path string) ([]agent.FSInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []agent.FSInfo
	prefix := strings.TrimSuffix(path, "/") + "/"
	for k, b := range m.files {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		if strings.Contains(rest, "/") {
			continue
		}
		out = append(out, agent.FSInfo{Name: rest, Type: "file", Size: int64(len(b)), Mode: "0644"})
	}
	for d := range m.dirs {
		if !strings.HasPrefix(d, prefix) || d == path {
			continue
		}
		rest := strings.TrimPrefix(d, prefix)
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		out = append(out, agent.FSInfo{Name: rest, Type: "directory", Mode: "0755"})
	}
	return out, nil
}

func (m *memGuestFS) Mkdir(_ context.Context, path string, recursive bool, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mkdirs++
	_ = mode
	if recursive {
		// mark all parents
		p := path
		for p != "" && p != "/" {
			m.dirs[p] = true
			p = filepath.ToSlash(filepath.Dir(p))
			if p == "." {
				break
			}
		}
	}
	m.dirs[path] = true
	return nil
}

func (m *memGuestFS) Remove(_ context.Context, path string, recursive bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rms++
	_ = recursive
	delete(m.files, path)
	delete(m.dirs, path)
	return nil
}

func (m *memGuestFS) PutFile(_ context.Context, path string, r io.Reader, size int64, opts agent.CPOpts) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.failPutPath != "" && path == m.failPutPath {
		return fmt.Errorf("injected put failure")
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	_ = size
	_ = opts
	m.files[path] = b
	// ensure parent dir
	m.dirs[filepath.ToSlash(filepath.Dir(path))] = true
	return nil
}

func (m *memGuestFS) GetFile(_ context.Context, path string, w io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	b, ok := m.files[path]
	if !ok {
		return fmt.Errorf("get: not found")
	}
	_, err := w.Write(b)
	return err
}

func TestSafeRelJoin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got, err := safeRelJoin(root, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, filepath.Join("a", "b.txt")) {
		t.Fatalf("got %q", got)
	}
	for _, bad := range []string{"../escape", "/abs", "..", "a/../../x"} {
		if _, err := safeRelJoin(root, bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestSafeGuestJoin(t *testing.T) {
	t.Parallel()
	got, err := safeGuestJoin("/work/proj", "src/a.go")
	if err != nil || got != "/work/proj/src/a.go" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := safeGuestJoin("/work", "../etc/passwd"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := safeGuestJoin("/work", "/abs"); err == nil {
		t.Fatal("expected absolute error")
	}
}

func TestApplyConflictBarrierNoWrites(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	st := newSyncState("local", "vm", host, "/work")
	statePath := filepath.Join(t.TempDir(), "state.json")
	// Pre-seed state file that must remain unchanged.
	st.setEntry("x", &syncFingerprint{Type: "file", Size: 1, Mtime: 1}, &syncFingerprint{Type: "file", Size: 1, Mtime: 2})
	if err := saveSyncState(statePath, st); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(statePath)

	plan := &syncPlan{
		Items: []syncPlanItem{{
			RelPath: "x",
			Action:  syncActConflict,
			Reason:  "both changed",
			Source:  inv(2, 3, "0644"),
			Dest:    inv(3, 4, "0644"),
		}},
		Conflicts: 1,
	}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb:      syncPush,
		HostRoot:  host,
		GuestRoot: "/work",
		FS:        fs,
		State:     st,
		StatePath: statePath,
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err %v", err)
	}
	if res.ExitCode != syncExitConflict || fs.puts != 0 {
		t.Fatalf("res=%+v puts=%d", res, fs.puts)
	}
	after, _ := os.ReadFile(statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("state file changed despite conflicts")
	}
}

func TestApplyPushFileAndCrashModelA(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "a.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	statePath := filepath.Join(t.TempDir(), "state.json")
	// Existing on-disk state that should survive failed apply.
	prior := newSyncState("local", "vm", host, "/work")
	prior.setEntry("old", &syncFingerprint{Type: "file", Size: 1, Mtime: 1}, &syncFingerprint{Type: "file", Size: 1, Mtime: 1})
	if err := saveSyncState(statePath, prior); err != nil {
		t.Fatal(err)
	}

	// First op succeeds, second fails → state must remain prior.
	if err := os.WriteFile(filepath.Join(host, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs.failPutPath = "/work/b.txt"
	plan := &syncPlan{
		Items: []syncPlanItem{
			{RelPath: "a.txt", Action: syncActCreate, Source: inv(7, 1, "0644")},
			{RelPath: "b.txt", Action: syncActCreate, Source: inv(1, 1, "0644")},
		},
		Created: 2,
	}
	// Fresh in-memory state (disk still has prior).
	st := newSyncState("local", "vm", host, "/work")
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb:      syncPush,
		HostRoot:  host,
		GuestRoot: "/work",
		FS:        fs,
		State:     st,
		StatePath: statePath,
	})
	if err == nil {
		t.Fatal("expected apply error")
	}
	if res.ExitCode != syncExitApply {
		t.Fatalf("exit %d", res.ExitCode)
	}
	// Disk state still prior
	loaded, err := loadSyncState(statePath)
	if err != nil || loaded == nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := loaded.entry("old"); !ok {
		t.Fatal("prior entry lost")
	}
	if _, ok := loaded.entry("a.txt"); ok {
		t.Fatal("partial apply must not write state")
	}
	// Guest has a.txt from first put
	if _, ok := fs.files["/work/a.txt"]; !ok {
		t.Fatal("expected partial guest write")
	}
}

func TestApplyPushSuccessWritesState(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "a.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	st := newSyncState("local", "vm", host, "/work")
	statePath := filepath.Join(t.TempDir(), "state.json")
	plan := &syncPlan{
		Items: []syncPlanItem{
			{RelPath: "a.txt", Action: syncActCreate, Source: inv(2, 1, "0600")},
		},
		Created: 1,
	}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb:      syncPush,
		HostRoot:  host,
		GuestRoot: "/work",
		FS:        fs,
		State:     st,
		StatePath: statePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Dirty || res.Applied != 1 {
		t.Fatalf("%+v", res)
	}
	if string(fs.files["/work/a.txt"]) != "hi" {
		t.Fatalf("guest body %q", fs.files["/work/a.txt"])
	}
	loaded, err := loadSyncState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := loaded.entry("a.txt")
	if !ok || e.Host == nil || e.Guest == nil {
		t.Fatalf("baseline %+v", e)
	}
	if e.Host.Size != 2 {
		t.Fatalf("host size %d", e.Host.Size)
	}
}

func TestApplyPullFile(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	fs.files["/work/g.txt"] = []byte("from-guest")
	st := newSyncState("local", "vm", host, "/work")
	plan := &syncPlan{
		Items: []syncPlanItem{
			{RelPath: "g.txt", Action: syncActCreate, Source: inv(10, 5, "0644")},
		},
		Created: 1,
	}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb:      syncPull,
		HostRoot:  host,
		GuestRoot: "/work",
		FS:        fs,
		State:     st,
		StatePath: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 1 {
		t.Fatalf("%+v", res)
	}
	b, err := os.ReadFile(filepath.Join(host, "g.txt"))
	if err != nil || string(b) != "from-guest" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestApplyDeletePush(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	fs.files["/work/gone.txt"] = []byte("x")
	st := newSyncState("local", "vm", host, "/work")
	st.setEntry("gone.txt",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1},
	)
	plan := &syncPlan{
		Items: []syncPlanItem{
			{RelPath: "gone.txt", Action: syncActDelete, Dest: inv(1, 1, "0644")},
		},
		Deleted: 1,
	}
	_, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb:      syncPush,
		HostRoot:  host,
		GuestRoot: "/work",
		FS:        fs,
		State:     st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.files["/work/gone.txt"]; ok {
		t.Fatal("expected guest delete")
	}
	if _, ok := st.entry("gone.txt"); ok {
		t.Fatal("expected state remove")
	}
}

func TestApplyColdStartBaselineDirty(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	st := newSyncState("local", "vm", host, "/work")
	plan := &syncPlan{
		Items: []syncPlanItem{{
			RelPath:       "same.txt",
			Action:        syncActSkip,
			BaselineDirty: true,
			Source:        inv(5, 1, "0644"),
			Dest:          inv(5, 9, "0644"),
		}},
		Skipped:       1,
		BaselineDirty: 1,
	}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb:      syncPush,
		HostRoot:  host,
		GuestRoot: "/work",
		FS:        fs,
		State:     st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Dirty || fs.puts != 0 {
		t.Fatalf("dirty=%v puts=%d", res.Dirty, fs.puts)
	}
	if _, ok := st.entry("same.txt"); !ok {
		t.Fatal("expected baseline")
	}
}

func TestApplyDirCreatePush(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	_ = os.MkdirAll(filepath.Join(host, "sub"), 0o755)
	fs := newMemGuestFS()
	st := newSyncState("local", "vm", host, "/work")
	plan := &syncPlan{
		Items: []syncPlanItem{{
			RelPath: "sub",
			Action:  syncActCreate,
			Source:  &syncInvEntry{Type: "directory", Mode: "0755"},
		}},
		Created: 1,
	}
	_, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb:      syncPush,
		HostRoot:  host,
		GuestRoot: "/work",
		FS:        fs,
		State:     st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !fs.dirs["/work/sub"] {
		t.Fatalf("dirs=%v", fs.dirs)
	}
}

func TestApplySyncPlanValidation(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	plan := &syncPlan{}
	if _, err := applySyncPlan(context.Background(), nil, syncApplyOpts{FS: fs, HostRoot: host, GuestRoot: "/w"}); err == nil {
		t.Fatal("nil plan")
	}
	if _, err := applySyncPlan(context.Background(), plan, syncApplyOpts{HostRoot: host, GuestRoot: "/w"}); err == nil {
		t.Fatal("nil FS")
	}
	if _, err := applySyncPlan(context.Background(), plan, syncApplyOpts{FS: fs, HostRoot: "", GuestRoot: "/w"}); err == nil {
		t.Fatal("empty roots")
	}
	// Nil state is auto-created; empty plan succeeds.
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs, State: nil,
	})
	if err != nil || res.Applied != 0 {
		t.Fatalf("empty plan: %+v %v", res, err)
	}
}

func TestApplyUpdateModePullAndPush(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	path := filepath.Join(host, "m.txt")
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	fs.files["/work/m.txt"] = []byte("body")
	st := newSyncState("local", "vm", host, "/work")

	// Pull: chmod host file.
	plan := &syncPlan{Items: []syncPlanItem{{
		RelPath: "m.txt", Action: syncActUpdateMode,
		Source: inv(4, 1, "0600"), Dest: inv(4, 1, "0644"),
	}}}
	var prog []ProgressEvent
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPull, HostRoot: host, GuestRoot: "/work", FS: fs, State: st,
		OnProgress: func(e ProgressEvent) { prog = append(prog, e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 1 {
		t.Fatalf("applied=%d", res.Applied)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}
	if len(prog) != 1 || prog[0].Phase != "chmod" {
		t.Fatalf("progress %+v", prog)
	}

	// Push: update_mode re-Puts with new mode.
	st2 := newSyncState("local", "vm", host, "/work")
	plan2 := &syncPlan{Items: []syncPlanItem{{
		RelPath: "m.txt", Action: syncActUpdateMode,
		Source: inv(4, 1, "0755"), Dest: inv(4, 1, "0644"),
	}}}
	res2, err := applySyncPlan(context.Background(), plan2, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs, State: st2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Applied != 1 || fs.puts < 1 {
		t.Fatalf("push update_mode applied=%d puts=%d", res2.Applied, fs.puts)
	}
}

func TestApplyDeletePullAndDirCreatePull(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	gone := filepath.Join(host, "gone.txt")
	if err := os.WriteFile(gone, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	fs.dirs["/work"] = true
	fs.dirs["/work/subdir"] = true
	st := newSyncState("local", "vm", host, "/work")
	st.setEntry("gone.txt",
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1},
		&syncFingerprint{Type: "file", Size: 1, Mtime: 1},
	)
	plan := &syncPlan{
		Items: []syncPlanItem{
			{RelPath: "gone.txt", Action: syncActDelete, Dest: inv(1, 1, "0644")},
			{RelPath: "subdir", Action: syncActCreate, Source: &syncInvEntry{Type: "directory", Mode: "0700"}},
			{RelPath: "nested/deep", Action: syncActCreate, Source: &syncInvEntry{Type: "directory", Mode: "0755"}},
		},
		Deleted: 1, Created: 2,
	}
	// Ensure nested source dirs exist on guest for inventory-like Source.
	fs.dirs["/work/nested"] = true
	fs.dirs["/work/nested/deep"] = true

	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPull, HostRoot: host, GuestRoot: "/work", FS: fs, State: st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != 3 {
		t.Fatalf("applied=%d", res.Applied)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatal("expected host delete")
	}
	if st2, err := os.Stat(filepath.Join(host, "subdir")); err != nil || !st2.IsDir() {
		t.Fatalf("subdir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(host, "nested", "deep")); err != nil {
		t.Fatal(err)
	}
}

func TestApplyDeepestDeletesFirst(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	fs.files["/work/a/b/c.txt"] = []byte("x")
	fs.dirs["/work/a"] = true
	fs.dirs["/work/a/b"] = true
	st := newSyncState("local", "vm", host, "/work")
	// Deletes should sort deepest first via depthKey.
	plan := &syncPlan{
		Items: []syncPlanItem{
			{RelPath: "a", Action: syncActDelete},
			{RelPath: "a/b/c.txt", Action: syncActDelete},
			{RelPath: "a/b", Action: syncActDelete},
		},
		Deleted: 3,
	}
	_, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs, State: st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fs.rms != 3 {
		t.Fatalf("rms=%d", fs.rms)
	}
}

func TestApplyContextCancel(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &syncPlan{Items: []syncPlanItem{
		{RelPath: "a.txt", Action: syncActCreate, Source: inv(1, 1, "0644")},
	}}
	res, err := applySyncPlan(ctx, plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs,
		State: newSyncState("l", "v", host, "/work"),
	})
	if err == nil {
		t.Fatal("expected cancel")
	}
	if res.ExitCode != syncExitApply {
		t.Fatalf("exit %d", res.ExitCode)
	}
}

func TestApplyContextCancelOnDelete(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	fs.files["/work/x"] = []byte("x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &syncPlan{Items: []syncPlanItem{{RelPath: "x", Action: syncActDelete}}}
	res, err := applySyncPlan(ctx, plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs,
		State: newSyncState("l", "v", host, "/work"),
	})
	if err == nil || res.ExitCode != syncExitApply {
		t.Fatalf("want cancel apply, got %v %+v", err, res)
	}
}

func TestApplyContextCancelOnDirCreate(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	_ = os.MkdirAll(filepath.Join(host, "d"), 0o755)
	fs := newMemGuestFS()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &syncPlan{Items: []syncPlanItem{{
		RelPath: "d", Action: syncActCreate, Source: &syncInvEntry{Type: "directory", Mode: "0755"},
	}}}
	res, err := applySyncPlan(ctx, plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs,
		State: newSyncState("l", "v", host, "/work"),
	})
	if err == nil || res.ExitCode != syncExitApply {
		t.Fatalf("want cancel, got %v %+v", err, res)
	}
}

func TestApplyColdStartBaselinePull(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	st := newSyncState("local", "vm", host, "/work")
	plan := &syncPlan{Items: []syncPlanItem{{
		RelPath: "same.txt", Action: syncActSkip, BaselineDirty: true,
		Source: inv(5, 1, "0644"), Dest: inv(5, 9, "0644"),
	}}}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPull, HostRoot: host, GuestRoot: "/work", FS: fs, State: st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Dirty {
		t.Fatal("expected dirty")
	}
	e, ok := st.entry("same.txt")
	if !ok || e.Host == nil || e.Guest == nil {
		t.Fatalf("baseline %+v ok=%v", e, ok)
	}
	// Pull: Source=guest, Dest=host → host fp from Dest, guest from Source
	if e.Host.Mtime != 9 || e.Guest.Mtime != 1 {
		t.Fatalf("host/guest mtimes %+v %+v", e.Host, e.Guest)
	}
}

func TestApplyKeptDestNoOp(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	st := newSyncState("local", "vm", host, "/work")
	plan := &syncPlan{Items: []syncPlanItem{{
		RelPath: "f", Action: syncActKeptDest, Reason: "dest ahead",
	}}}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs, State: st,
	})
	if err != nil || res.Applied != 0 || res.Dirty {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestApplyFileOpUnexpected(t *testing.T) {
	t.Parallel()
	err := applyFileOp(context.Background(), syncApplyOpts{Verb: syncPush}, syncPlanItem{
		RelPath: "x", Action: syncActSkip,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyFileTransferUnknownVerb(t *testing.T) {
	t.Parallel()
	err := applyFileTransfer(context.Background(), syncApplyOpts{Verb: "sideways"}, syncPlanItem{RelPath: "x"})
	if err == nil {
		t.Fatal("expected unknown verb")
	}
}

func TestApplyPushFileMissingHost(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	err := applyPushFile(context.Background(), syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs,
		State: newSyncState("l", "v", host, "/work"),
	}, syncPlanItem{RelPath: "nope.txt", Action: syncActCreate, Source: inv(1, 1, "0644")})
	if err == nil {
		t.Fatal("expected missing host file")
	}
}

func TestApplyPushFileIsDir(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	if err := os.Mkdir(filepath.Join(host, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	err := applyPushFile(context.Background(), syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs,
		State: newSyncState("l", "v", host, "/work"),
	}, syncPlanItem{RelPath: "d", Action: syncActCreate, Source: &syncInvEntry{Type: "file", Size: 0}})
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("got %v", err)
	}
}

func TestApplyPullFileStatFail(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	fs := newMemGuestFS()
	err := applyPullFile(context.Background(), syncApplyOpts{
		Verb: syncPull, HostRoot: host, GuestRoot: "/work", FS: fs,
		State: newSyncState("l", "v", host, "/work"),
	}, syncPlanItem{RelPath: "missing.txt", Action: syncActCreate, Source: inv(1, 1, "0644")})
	if err == nil {
		t.Fatal("expected guest stat fail")
	}
}

func TestApplyReplacePush(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "f"), []byte("now-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	// Dest was a dir conceptually; replace still does file transfer.
	st := newSyncState("local", "vm", host, "/work")
	plan := &syncPlan{Items: []syncPlanItem{{
		RelPath: "f", Action: syncActReplace, Source: inv(8, 1, "0644"),
	}}}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs, State: st,
	})
	if err != nil || res.Applied != 1 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestDepthKeyAndHelpers(t *testing.T) {
	t.Parallel()
	if depthKey("") != 0 || depthKey("/") != 0 {
		t.Fatalf("empty depth")
	}
	if depthKey("a") != 1 || depthKey("a/b/c") != 3 {
		t.Fatalf("depth a=%d a/b/c=%d", depthKey("a"), depthKey("a/b/c"))
	}
	if parseOctalMode("", 0o644) != 0o644 {
		t.Fatal("empty mode def")
	}
	if parseOctalMode("not-octal", 0o755) != 0o755 {
		t.Fatal("invalid mode def")
	}
	if parseOctalMode("0o644", 0) != 0o644 {
		t.Fatal("0o prefix")
	}
	if fsInfoToFingerprint(nil) != nil {
		t.Fatal("nil fs info")
	}
	fp := fsInfoToFingerprint(&agent.FSInfo{Type: "file", Size: 3, Mtime: 9, Mode: "0644"})
	if fp.Size != 3 || fp.Mtime != 9 {
		t.Fatalf("%+v", fp)
	}
	n, err := (discardWriter{}).Write([]byte("hi"))
	if err != nil || n != 2 {
		t.Fatalf("discard %d %v", n, err)
	}
}

func TestLstatToFingerprintTypes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	fp := lstatToFingerprint(fi)
	if fp.Type != "directory" {
		t.Fatalf("dir type %s", fp.Type)
	}
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Lstat(f)
	fp = lstatToFingerprint(fi)
	if fp.Type != "file" || fp.Size != 1 {
		t.Fatalf("%+v", fp)
	}
	link := filepath.Join(dir, "l")
	if err := os.Symlink("f", link); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Lstat(link)
	fp = lstatToFingerprint(fi)
	if fp.Type != "symlink" {
		t.Fatalf("symlink type %s", fp.Type)
	}
}

func TestSafeJoinEdgeCases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	got, err := safeRelJoin(root, "")
	if err != nil || got != filepath.Clean(root) {
		t.Fatalf("empty rel: %q %v", got, err)
	}
	got, err = safeRelJoin(root, ".")
	if err != nil || got != filepath.Clean(root) {
		t.Fatalf("dot: %q %v", got, err)
	}
	got, err = safeGuestJoin("/work", "")
	if err != nil || got != "/work" {
		t.Fatalf("guest empty: %q %v", got, err)
	}
	got, err = safeGuestJoin("/", "a")
	if err != nil || got != "/a" {
		t.Fatalf("guest root: %q %v", got, err)
	}
}

func TestRefreshBaselineBothPull(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	sub := filepath.Join(host, "d")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	fs.dirs["/work/d"] = true
	st := newSyncState("l", "v", host, "/work")
	opts := syncApplyOpts{Verb: syncPull, HostRoot: host, GuestRoot: "/work", FS: fs, State: st}
	err := refreshBaselineBoth(context.Background(), opts, "d", syncPlanItem{
		Source: &syncInvEntry{Type: "directory", Mode: "0755"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e, ok := st.entry("d")
	if !ok || e.Host == nil || e.Guest == nil {
		t.Fatalf("%+v", e)
	}
}

func TestApplyDirCreatePushStatFallback(t *testing.T) {
	t.Parallel()
	// FS.Mkdir succeeds but Stat fails → refreshBaselineBoth push fallback.
	host := t.TempDir()
	_ = os.MkdirAll(filepath.Join(host, "sub"), 0o755)
	fs := &statFailAfterMkdirFS{memGuestFS: *newMemGuestFS()}
	st := newSyncState("local", "vm", host, "/work")
	plan := &syncPlan{Items: []syncPlanItem{{
		RelPath: "sub", Action: syncActCreate,
		Source: &syncInvEntry{Type: "directory", Mode: "0755"},
	}}}
	_, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs, State: st,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.entry("sub"); !ok {
		t.Fatal("expected baseline from fallback")
	}
}

// statFailAfterMkdirFS Mkdirs OK but Stat always fails (covers refresh fallback).
type statFailAfterMkdirFS struct {
	memGuestFS
}

func (s *statFailAfterMkdirFS) Stat(ctx context.Context, path string) (*agent.FSInfo, error) {
	return nil, fmt.Errorf("stat always fails")
}

func (s *statFailAfterMkdirFS) Mkdir(ctx context.Context, path string, recursive bool, mode string) error {
	return s.memGuestFS.Mkdir(ctx, path, recursive, mode)
}

func TestApplyStateSaveError(t *testing.T) {
	t.Parallel()
	host := t.TempDir()
	if err := os.WriteFile(filepath.Join(host, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newMemGuestFS()
	st := newSyncState("local", "vm", host, "/work")
	// StatePath is a directory → rename/create fails.
	badPath := t.TempDir()
	plan := &syncPlan{Items: []syncPlanItem{
		{RelPath: "a.txt", Action: syncActCreate, Source: inv(2, 1, "0644")},
	}}
	res, err := applySyncPlan(context.Background(), plan, syncApplyOpts{
		Verb: syncPush, HostRoot: host, GuestRoot: "/work", FS: fs, State: st, StatePath: badPath,
	})
	if err == nil {
		t.Fatal("expected state save error")
	}
	if res.ExitCode != syncExitApply {
		t.Fatalf("exit %d", res.ExitCode)
	}
}

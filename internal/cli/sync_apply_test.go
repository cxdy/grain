package cli

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
	if size >= 0 && int64(len(b)) != size && size != 0 {
		// allow
	}
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

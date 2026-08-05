package vmsync

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
)

func stringReader(s string) io.Reader { return strings.NewReader(s) }

func agentCP() agent.CPOpts { return agent.CPOpts{Mode: "0644"} }

func TestInventoryHostBasic(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ign, err := buildSyncIgnore(syncIgnoreOpts{HostRoot: dir})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := InventoryHost(dir, ign)
	if err != nil {
		t.Fatal(err)
	}
	if inv["a.go"] == nil || inv["a.go"].Type != "file" {
		t.Fatalf("a.go: %+v", inv["a.go"])
	}
	if inv["sub"] == nil || inv["sub"].Type != "directory" {
		t.Fatalf("sub: %+v", inv["sub"])
	}
	if inv["sub/b.txt"] == nil {
		t.Fatalf("missing sub/b.txt: %v", inv)
	}
	if inv[".git/config"] != nil {
		t.Fatalf(".git should be ignored, got %v", inv[".git/config"])
	}
}

func TestInventoryGuestBFS(t *testing.T) {
	m := newMemGuestFS()
	ctx := context.Background()
	_ = m.Mkdir(ctx, "/work", true, "0755")
	_ = m.Mkdir(ctx, "/work/sub", true, "0755")
	_ = m.PutFile(ctx, "/work/a.txt", stringReader("hello"), 5, agentCP())
	_ = m.PutFile(ctx, "/work/sub/b.txt", stringReader("x"), 1, agentCP())

	inv, err := InventoryGuest(ctx, m, "/work", nil)
	if err != nil {
		t.Fatal(err)
	}
	if inv["a.txt"] == nil || inv["a.txt"].Size != 5 {
		t.Fatalf("a.txt: %+v", inv["a.txt"])
	}
	if inv["sub"] == nil || inv["sub"].Type != "directory" {
		t.Fatalf("sub: %+v", inv["sub"])
	}
	if inv["sub/b.txt"] == nil {
		t.Fatalf("sub/b.txt missing: %v", inv)
	}
}

func TestInventoryHostSymlinkAndIgnoreFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.me"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("keep.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	// Directory symlink should be recorded and not descended.
	if err := os.Mkdir(filepath.Join(dir, "realdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realdir", "inside"), []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("realdir", filepath.Join(dir, "dlink")); err != nil {
		t.Fatal(err)
	}

	ign, err := buildSyncIgnore(syncIgnoreOpts{NoDefaults: true, ExtraLines: []string{"skip.me"}})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := InventoryHost(dir, ign)
	if err != nil {
		t.Fatal(err)
	}
	if inv["skip.me"] != nil {
		t.Fatal("skip.me should be ignored")
	}
	if inv["link"] == nil || inv["link"].Type != "symlink" || inv["link"].Target != "keep.txt" {
		t.Fatalf("link: %+v", inv["link"])
	}
	if inv["dlink"] == nil || inv["dlink"].Type != "symlink" {
		t.Fatalf("dlink: %+v", inv["dlink"])
	}
	// Must not walk through dir symlink.
	if inv["dlink/inside"] != nil {
		t.Fatal("should not descend dir symlink")
	}
	if inv["realdir/inside"] == nil {
		t.Fatal("realdir/inside missing")
	}
}

func TestInventoryGuestMissingAndNotDir(t *testing.T) {
	m := newMemGuestFS()
	ctx := context.Background()
	inv, err := InventoryGuest(ctx, m, "/missing", nil)
	if err != nil || len(inv) != 0 {
		t.Fatalf("missing root: %v %v", inv, err)
	}
	_ = m.PutFile(ctx, "/file", stringReader("x"), 1, agentCP())
	_, err = InventoryGuest(ctx, m, "/file", nil)
	if err == nil {
		t.Fatal("expected not-directory error")
	}
}

func TestInventoryGuestIgnoreAndCancel(t *testing.T) {
	m := newMemGuestFS()
	ctx := context.Background()
	_ = m.Mkdir(ctx, "/work", true, "0755")
	_ = m.Mkdir(ctx, "/work/build", true, "0755")
	_ = m.PutFile(ctx, "/work/build/out.o", stringReader("o"), 1, agentCP())
	_ = m.PutFile(ctx, "/work/skip.me", stringReader("s"), 1, agentCP())
	_ = m.PutFile(ctx, "/work/keep.txt", stringReader("k"), 1, agentCP())
	// Empty type treated as file.
	m.files["/work/emptytype"] = []byte("e")

	ign, err := buildSyncIgnore(syncIgnoreOpts{NoDefaults: true, ExtraLines: []string{"build/", "skip.me"}})
	if err != nil {
		t.Fatal(err)
	}
	// Force empty Type on one ReadDir entry by using mem FS which sets type file — covered by default.
	inv, err := InventoryGuest(ctx, m, "/work", ign)
	if err != nil {
		t.Fatal(err)
	}
	if inv["keep.txt"] == nil {
		t.Fatal("keep missing")
	}
	if inv["skip.me"] != nil || inv["build"] != nil || inv["build/out.o"] != nil {
		t.Fatalf("ignored still present: %v", inv)
	}

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = InventoryGuest(cctx, m, "/work", nil)
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func TestInventoryGuestReadDirError(t *testing.T) {
	fs := &readDirFailFS{memGuestFS: *newMemGuestFS()}
	_ = fs.Mkdir(context.Background(), "/work", true, "0755")
	_, err := InventoryGuest(context.Background(), fs, "/work", nil)
	if err == nil {
		t.Fatal("expected readdir error")
	}
}

type readDirFailFS struct {
	memGuestFS
}

func (r *readDirFailFS) ReadDir(ctx context.Context, path string) ([]agent.FSInfo, error) {
	return nil, os.ErrPermission
}

func (r *readDirFailFS) Stat(ctx context.Context, path string) (*agent.FSInfo, error) {
	return r.memGuestFS.Stat(ctx, path)
}

func TestParseArgsPushPull(t *testing.T) {
	parse := func(s string) (bool, string, string) {
		// Mirror parseCPSpec: NAME:path when name has no /
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
	host, vm, guest, err := ParseArgs(Push, "/tmp/h", "lab:/work/p", parse)
	if err != nil || host != "/tmp/h" || vm != "lab" || guest != "/work/p" {
		t.Fatalf("push: host=%q vm=%q guest=%q err=%v", host, vm, guest, err)
	}
	host, vm, guest, err = ParseArgs(Pull, "lab:/work/p", "/tmp/h", parse)
	if err != nil || host != "/tmp/h" || vm != "lab" || guest != "/work/p" {
		t.Fatalf("pull: host=%q vm=%q guest=%q err=%v", host, vm, guest, err)
	}
	if _, _, _, err := ParseArgs(Push, "lab:/x", "/tmp/h", parse); err == nil {
		t.Fatal("expected push swapped args error")
	}
	if _, _, _, err := ParseArgs(Pull, "/tmp/h", "lab:/x", parse); err == nil {
		t.Fatal("expected pull swapped args error")
	}
}

func containsSlash(s string) bool {
	for _, c := range s {
		if c == '/' {
			return true
		}
	}
	return false
}

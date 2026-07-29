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

func agentCP(mode string) agent.CPOpts { return agent.CPOpts{Mode: mode} }

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
	_ = m.PutFile(ctx, "/work/a.txt", stringReader("hello"), 5, agentCP("0644"))
	_ = m.PutFile(ctx, "/work/sub/b.txt", stringReader("x"), 1, agentCP("0644"))

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

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncStateIDNoDirection(t *testing.T) {
	t.Parallel()
	a := syncStateID("local:/tmp/grain.sock", "vm1", "/host/proj", "/work/proj")
	b := syncStateID("local:/tmp/grain.sock", "vm1", "/host/proj", "/work/proj")
	if a != b || len(a) != 32 {
		t.Fatalf("id %q len %d", a, len(a))
	}
	// Different roots → different id
	c := syncStateID("local:/tmp/grain.sock", "vm1", "/host/other", "/work/proj")
	if a == c {
		t.Fatal("expected different id for different host root")
	}
	// api identity matters
	d := syncStateID("http://192.168.4.108:7474", "vm1", "/host/proj", "/work/proj")
	if a == d {
		t.Fatal("expected different id for remote api")
	}
}

func TestSyncStateRoundTripHostGuestKeys(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sync", "abcd.json")
	st := newSyncState("local:sock", "pmm-loki", "/Users/me/proj", "/work/proj")
	st.setEntry("src/main.go", &syncFingerprint{
		Type: "file", Size: 100, Mtime: 1000, Mode: "0644",
	}, &syncFingerprint{
		Type: "file", Size: 100, Mtime: 2000, Mode: "0644", // different mtime OK
	})
	if err := saveSyncState(path, st); err != nil {
		t.Fatal(err)
	}
	// mode 0600
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", fi.Mode().Perm())
	}

	got, err := loadSyncState(path)
	if err != nil || got == nil {
		t.Fatalf("load: %v %+v", err, got)
	}
	e, ok := got.entry("src/main.go")
	if !ok {
		t.Fatal("missing entry")
	}
	if e.Host == nil || e.Guest == nil {
		t.Fatalf("need host+guest keys: %+v", e)
	}
	if e.Host.Mtime != 1000 || e.Guest.Mtime != 2000 {
		t.Fatalf("mtimes host=%d guest=%d", e.Host.Mtime, e.Guest.Mtime)
	}
	// incomplete entry is not a B
	got.Entries["half"] = syncEntry{Host: &syncFingerprint{Type: "file", Size: 1, Mtime: 1}}
	if _, ok := got.entry("half"); ok {
		t.Fatal("incomplete should not count as B")
	}
}

func TestLoadSyncStateMissing(t *testing.T) {
	t.Parallel()
	st, err := loadSyncState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || st != nil {
		t.Fatalf("got %v %v", st, err)
	}
}

func TestSyncStatePathAndRemoveEntry(t *testing.T) {
	t.Parallel()
	id := syncStateID("local:x", "vm", "/h", "/g")
	p := syncStatePath("/data/grain", id)
	want := filepath.Join("/data/grain", "sync", id+".json")
	if p != want {
		t.Fatalf("path %q want %q", p, want)
	}
	st := newSyncState("local:x", "vm", "/h", "/g")
	st.setEntry("gone", &syncFingerprint{Type: "file", Size: 1, Mtime: 1}, &syncFingerprint{Type: "file", Size: 1, Mtime: 2})
	st.removeEntry("gone")
	if _, ok := st.entry("gone"); ok {
		t.Fatal("removeEntry should drop path")
	}
}

func TestFpContentEqualIgnoresMode(t *testing.T) {
	t.Parallel()
	a := &syncFingerprint{Type: "file", Size: 10, Mtime: 5, Mode: "0644"}
	b := &syncFingerprint{Type: "file", Size: 10, Mtime: 5, Mode: "0755"}
	if !fpContentEqual(a, b) {
		t.Fatal("mode should not affect content fp")
	}
	b.Mtime = 6
	if fpContentEqual(a, b) {
		t.Fatal("mtime should affect content fp")
	}
	da := &syncFingerprint{Type: "directory", Size: 0, Mtime: 1}
	db := &syncFingerprint{Type: "directory", Size: 99, Mtime: 99}
	if !fpContentEqual(da, db) {
		t.Fatal("dirs: type only")
	}
}

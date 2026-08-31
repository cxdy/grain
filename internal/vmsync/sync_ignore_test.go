package vmsync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncIgnoreDefaults(t *testing.T) {
	t.Parallel()
	ign, err := buildSyncIgnore(syncIgnoreOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".git/config", ".git/HEAD", ".github/workflows/ci.yml", ".vscode/settings.json"} {
		if ign.Match(p) || ign.MatchDir(filepath.Dir(p)) {
			t.Fatalf("hidden path %q should not be ignored by defaults", p)
		}
	}
	if ign.Match("src/main.go") {
		t.Fatal("src should not be ignored")
	}
}

func TestSyncIgnoreNoDefaults(t *testing.T) {
	t.Parallel()
	ign, err := buildSyncIgnore(syncIgnoreOpts{NoDefaults: true})
	if err != nil {
		t.Fatal(err)
	}
	if ign.Match(".git/config") {
		t.Fatal("no-defaults should not ignore .git")
	}
}

func TestSyncIgnoreExcludeGit(t *testing.T) {
	t.Parallel()
	ign, err := buildSyncIgnore(syncIgnoreOpts{Exclude: []string{".git/", "**/.git/"}})
	if err != nil {
		t.Fatal(err)
	}
	if !ign.Match(".git/config") && !ign.MatchDir(".git") {
		if !ign.Match(".git/config") {
			t.Fatal("expected .git/config ignored by --exclude")
		}
	}
	if ign.Match(".github/workflows/ci.yml") {
		t.Fatal(".github should not be ignored by a .git/ exclude")
	}
}

func TestSyncIgnoreGrainignoreAndExclude(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".grainignore"), []byte("secret/\n*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("dist/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ign, err := buildSyncIgnore(syncIgnoreOpts{
		HostRoot: root,
		Exclude:  []string{"vendor/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"secret/x", "foo.tmp", "dist/a", "vendor/pkg"} {
		if !ign.Match(p) && !ign.MatchDir(filepath.Dir(p)) {
			// try Match on path
			if !ign.Match(p) {
				t.Fatalf("expected ignore %q", p)
			}
		}
	}
	if ign.Match("src/ok.go") {
		t.Fatal("src/ok.go should not be ignored")
	}
}

func TestSyncIgnoreDeleteEligible(t *testing.T) {
	t.Parallel()
	ign, err := buildSyncIgnore(syncIgnoreOpts{
		NoDefaults: true,
		ExtraLines: []string{"build/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ign.deleteEligible("build/out") {
		t.Fatal("ignored dest must not be delete-eligible")
	}
	if !ign.deleteEligible("src/main.go") {
		t.Fatal("normal path should be delete-eligible")
	}
}

func TestSyncIgnoreRootNeverMatched(t *testing.T) {
	t.Parallel()
	ign, err := buildSyncIgnore(syncIgnoreOpts{ExtraLines: []string{"*"}})
	if err != nil {
		t.Fatal(err)
	}
	if ign.Match("") || ign.Match(".") {
		t.Fatal("root must not be ignored")
	}
}

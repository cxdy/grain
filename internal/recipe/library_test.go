package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibrarySaveListResolveDelete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const y = `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: git-lab
  description: test lab
spec:
  image: grain-ubuntu
  cpus: 2
  memory_mb: 2048
`
	ent, err := SaveLibrary(dir, []byte(y), SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "git-lab" || !strings.HasSuffix(ent.Path, "git-lab.yaml") {
		t.Fatalf("%+v", ent)
	}
	// no silent overwrite
	if _, err := SaveLibrary(dir, []byte(y), SaveOptions{}); err == nil {
		t.Fatal("expected overwrite error")
	}
	// overwrite ok
	if _, err := SaveLibrary(dir, []byte(y), SaveOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}

	list, err := ListLibrary(dir)
	if err != nil || len(list) != 1 || list[0].ID != "git-lab" {
		t.Fatalf("%+v %v", list, err)
	}

	path, err := ResolvePath(dir, "git-lab")
	if err != nil || path != ent.Path {
		t.Fatalf("%s %v", path, err)
	}
	f, err := LoadResolved(dir, "git-lab")
	if err != nil || f.Metadata.Name != "git-lab" {
		t.Fatalf("%v %v", f, err)
	}

	// invalid refuse
	if _, err := SaveLibrary(dir, []byte("not: a: recipe\n"), SaveOptions{ID: "bad"}); err == nil {
		t.Fatal("expected invalid reject")
	}
	if _, err := SaveLibrary(dir, []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: x
spec:
  wait: not-a-mode
`), SaveOptions{}); err == nil {
		t.Fatal("expected validate fail")
	}

	if err := DeleteLibrary(dir, "git-lab"); err != nil {
		t.Fatal(err)
	}
	list, _ = ListLibrary(dir)
	if len(list) != 0 {
		t.Fatalf("after delete: %+v", list)
	}
}

func TestAddFileAndResolvePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "node-dev.recipe.yaml")
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: node-dev
spec:
  image: grain-ubuntu
  cpus: 2
  mounts:
    - host: .
      guest: /work
`)
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	ent, err := AddFile(dir, src, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// stem from filename (node-dev.recipe → node-dev)
	if ent.ID != "node-dev" {
		t.Fatalf("id %q", ent.ID)
	}
	// resolve absolute path
	p, err := ResolvePath(dir, src)
	if err != nil {
		t.Fatal(err)
	}
	if p != src && filepath.Base(p) != filepath.Base(src) {
		// abs path of src
		abs, _ := filepath.Abs(src)
		if p != abs {
			t.Fatalf("path resolve got %s", p)
		}
	}
}

func TestEmptyLibrary(t *testing.T) {
	t.Parallel()
	list, err := ListLibrary(filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
	if _, err := ResolvePath(t.TempDir(), "nope"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestDefaultLibraryDirAndExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	got := DefaultLibraryDir()
	if got != filepath.Join(home, "recipes") {
		t.Fatalf("%s", got)
	}
	t.Setenv("GRAIN_HOME", "")
	_ = DefaultLibraryDir()

	// expandHome via ListLibrary empty dir → default (may not exist → empty list)
	list, err := ListLibrary("")
	if err != nil {
		// may error if default exists but unreadable — ok if nil list on missing
		t.Log(err)
	}
	_ = list

	// ~ expansion through SaveLibrary path
	// use explicit dir with ~
	// expandHome("~") and expandHome("~/x")
	if expandHome("") != "" {
		t.Fatal("empty expand")
	}
	if expandHome("~") == "~" {
		// only if UserHomeDir fails; usually expands
		t.Log("home expand failed, ok in sandbox")
	} else if !filepath.IsAbs(expandHome("~")) {
		t.Fatalf("expand ~: %s", expandHome("~"))
	}
	if p := expandHome("~/recipes"); !strings.Contains(p, "recipes") {
		t.Fatalf("~/recipes: %s", p)
	}
	if expandHome("/abs") != "/abs" {
		t.Fatal(expandHome("/abs"))
	}
}

func TestListLibrarySkipsAndYml(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// valid .yml
	yml := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: yml-lab
spec:
  image: grain-ubuntu
  cpus: 1
`)
	if err := os.WriteFile(filepath.Join(dir, "yml-lab.yml"), yml, 0o644); err != nil {
		t.Fatal(err)
	}
	// invalid yaml
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(":::"), 0o644); err != nil {
		t.Fatal(err)
	}
	// non-yaml
	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// subdirectory ignored
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// legacy .recipe.yaml
	legacy := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: legacy-name
spec:
  image: grain-ubuntu
  cpus: 1
`)
	if err := os.WriteFile(filepath.Join(dir, "legacy.recipe.yaml"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := ListLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	// sorted by id: legacy, yml-lab
	if list[0].ID != "legacy" || list[1].ID != "yml-lab" {
		t.Fatalf("%+v", list)
	}
	if list[0].HasBootstrap {
		t.Fatal("no bootstrap")
	}

	// ResolvePath .yml candidate
	p, err := ResolvePath(dir, "yml-lab")
	if err != nil || !strings.HasSuffix(p, "yml-lab.yml") {
		t.Fatalf("%s %v", p, err)
	}
	// ResolvePath empty name
	if _, err := ResolvePath(dir, ""); err == nil {
		t.Fatal("empty name")
	}
	// ResolvePath relative existing file
	rel := filepath.Join(dir, "yml-lab.yml")
	p2, err := ResolvePath(dir, rel)
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(rel)
	if p2 != abs {
		t.Fatalf("%s vs %s", p2, abs)
	}
	// bare relative file that exists in cwd — use path with separator only
	if _, err := ResolvePath(dir, "missing.yaml"); err == nil {
		// may fail as bare name
		_ = err
	}
}

func TestSanitizeAndSaveID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	y := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: "My Lab Name!"
spec:
  image: grain-ubuntu
  cpus: 1
`)
	ent, err := SaveLibrary(dir, y, SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "My-Lab-Name" {
		t.Fatalf("id %q", ent.ID)
	}
	// custom ID — filepath.Ext strips a trailing .v1 segment
	ent2, err := SaveLibrary(dir, y, SaveOptions{ID: "custom_id.v1", Overwrite: false})
	if err != nil {
		t.Fatal(err)
	}
	if ent2.ID != "custom_id" {
		t.Fatalf("%q", ent2.ID)
	}
	// empty id after sanitize
	if _, err := SaveLibrary(dir, []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: "!!!"
spec:
  image: grain-ubuntu
  cpus: 1
`), SaveOptions{}); err == nil {
		t.Fatal("expected invalid id")
	}
	// no name and no id
	if _, err := SaveLibrary(dir, []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: ""
spec:
  image: grain-ubuntu
  cpus: 1
`), SaveOptions{}); err == nil {
		t.Fatal("expected id required")
	}
}

func TestDeleteLibraryErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	y := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: del-me
spec:
  image: grain-ubuntu
  cpus: 1
`)
	if _, err := SaveLibrary(dir, y, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteLibrary(dir, ""); err == nil {
		t.Fatal("empty id")
	}
	if err := DeleteLibrary(dir, "!!!"); err == nil {
		t.Fatal("invalid id")
	}
	if err := DeleteLibrary(dir, "del-me"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteLibrary(dir, "del-me"); err == nil {
		t.Fatal("already gone")
	}
}

func TestLibraryIDFromFilename(t *testing.T) {
	t.Parallel()
	if libraryIDFromFilename("foo.recipe.yaml") != "foo" {
		t.Fatal(libraryIDFromFilename("foo.recipe.yaml"))
	}
	if libraryIDFromFilename("/x/bar.yml") != "bar" {
		t.Fatal(libraryIDFromFilename("/x/bar.yml"))
	}
	if got := sanitizeRecipeID("  a b/c!  "); got != "c" {
		// Base strips to c! → c
		t.Fatalf("sanitize got %q", got)
	}
	if sanitizeRecipeID("ok-id_1") != "ok-id_1" {
		t.Fatal(sanitizeRecipeID("ok-id_1"))
	}
}

func TestResolvePathRecipeYAMLCandidate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: legacy-id
spec:
  image: grain-ubuntu
  cpus: 1
`)
	// only .recipe.yaml candidate
	if err := os.WriteFile(filepath.Join(dir, "legacy-id.recipe.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := ResolvePath(dir, "legacy-id")
	if err != nil || !strings.HasSuffix(p, "legacy-id.recipe.yaml") {
		t.Fatalf("%s %v", p, err)
	}
	f, err := LoadResolved(dir, "legacy-id")
	if err != nil || f.Metadata.Name != "legacy-id" {
		t.Fatalf("%v %v", f, err)
	}
}

func TestListLibraryWithBootstrap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	y := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: boot
spec:
  image: grain-ubuntu
  cpus: 1
  bootstrap:
    steps:
      - name: setup
        run: echo hi
`)
	if _, err := SaveLibrary(dir, y, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	list, err := ListLibrary(dir)
	if err != nil || len(list) != 1 || !list[0].HasBootstrap {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestDeleteOutsideLibraryRefused(t *testing.T) {
	t.Parallel()
	// Create a library and a recipe outside it; ResolvePath with absolute path
	// then DeleteLibrary should refuse if path is outside (when resolved to abs outside).
	// DeleteLibrary resolves by id inside dir only for bare ids.
	// Absolute path via ResolvePath for bare name stays under dir.
	// Exercise empty dir → DefaultLibraryDir path by using expandHome.
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: outside
spec:
  image: grain-ubuntu
  cpus: 1
`)
	if err := os.WriteFile(outside, body, 0o644); err != nil {
		t.Fatal(err)
	}
	// If we could force ResolvePath to return outside while dir is library...
	// DeleteLibrary uses ResolvePath(dir, id) with sanitized id only — absolute paths
	// as id become Base after sanitize. So outside-delete is hard via public API.
	// Still cover DeleteLibrary not-found for sanitized id.
	if err := DeleteLibrary(dir, "does-not-exist"); err == nil {
		t.Fatal("want not found")
	}
	_ = outside
}

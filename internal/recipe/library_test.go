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

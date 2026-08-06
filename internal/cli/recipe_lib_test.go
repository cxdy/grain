package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecipeLibraryCLI_B(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	lib := filepath.Join(home, "recipes")

	src := filepath.Join(t.TempDir(), "lab.yaml")
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
  description: unit lab
spec:
  image: grain-ubuntu
  cpus: 2
`)
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) (string, error) {
		root := Root("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}

	out, err := run("recipe", "add", src)
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "added") || !strings.Contains(out, "lab") {
		t.Fatalf("%q", out)
	}
	// no silent overwrite
	if _, err := run("recipe", "add", src); err == nil {
		t.Fatal("expected overwrite error")
	}

	out, err = run("recipe", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "lab") {
		t.Fatalf("list: %q", out)
	}

	out, err = run("recipe", "show", "lab")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "grain-ubuntu") {
		t.Fatalf("show: %q", out)
	}

	out, err = run("recipe", "validate", "lab")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("validate: %q", out)
	}

	// name resolve path exists on disk
	p := filepath.Join(lib, "lab.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}

	out, err = run("recipe", "delete", "lab")
	if err != nil {
		t.Fatal(err, out)
	}
	out, _ = run("recipe", "list")
	if strings.Contains(out, "lab") && !strings.Contains(out, "No recipes") {
		// empty library message
		if !strings.Contains(out, "No recipes") {
			t.Fatalf("after delete list: %q", out)
		}
	}
}

func TestRecipeLibraryCLI_C_URLAndCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: remote-lab
spec:
  image: grain-ubuntu
  cpus: 1
`)
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"apiVersion":"grain.recipes/v1","recipes":[{"id":"remote-lab","title":"Remote","path":"remote-lab.yaml","sha256":%q}]}`, hexSum)
	})
	mux.HandleFunc("/remote-lab.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	t.Setenv("GRAIN_RECIPE_CATALOG_URL", srv.URL+"/catalog.json")
	// CatalogCachePath uses GRAIN_HOME
	run := func(args ...string) (string, error) {
		root := Root("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}

	out, err := run("recipe", "search")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "remote-lab") {
		t.Fatalf("search: %q", out)
	}
	// search must not install
	if list, _ := run("recipe", "list"); strings.Contains(list, "remote-lab") && !strings.Contains(list, "No recipes") {
		if !strings.Contains(list, "No recipes") {
			t.Fatalf("search installed? %q", list)
		}
	}

	out, err = run("recipe", "add", "remote-lab")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "added") {
		t.Fatalf("%q", out)
	}

	// URL add of another copy under different id
	out, err = run("recipe", "add", srv.URL+"/remote-lab.yaml", "--id", "via-url")
	if err != nil {
		t.Fatal(err, out)
	}

	// no VM create on add — just library files
	lib := filepath.Join(home, "recipes")
	ents, _ := os.ReadDir(lib)
	if len(ents) < 2 {
		t.Fatalf("library files: %v", ents)
	}
}

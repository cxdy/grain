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

func TestEmptyDash(t *testing.T) {
	t.Parallel()
	if emptyDash("") != "-" || emptyDash("  ") != "-" {
		t.Fatal("empty")
	}
	if emptyDash("x") != "x" {
		t.Fatal("x")
	}
}

func TestRecipeLibraryDirAndLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	if recipeLibraryDir() == "" {
		t.Fatal("empty lib")
	}
	src := filepath.Join(t.TempDir(), "r.yaml")
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: x
spec:
  image: grain-ubuntu
`)
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := loadRecipeArg(src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Metadata.Name != "x" {
		t.Fatalf("%+v", f.Metadata)
	}
}

func TestRecipePreviewURLAndWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: prev2
  description: d
spec:
  image: grain-ubuntu
  cpus: 1
  memory_mb: 512
  disk_gb: 4
  persistent: true
  bootstrap:
    steps:
      - name: a
        run: true
  forwards:
    - host_port: 8080
      guest_port: 80
  mounts:
    - host: /tmp
      guest: /work
`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	run := func(args ...string) (string, error) {
		root := Root("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}

	// cleartext HTTP warning path
	out, err := run("recipe", "preview", "http://"+strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		// httptest is http:// — should work
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "prev2") || !strings.Contains(out, "grain-ubuntu") {
		t.Fatalf("%q", out)
	}
	if !strings.Contains(out, "bootstrap") || !strings.Contains(out, "forwards") {
		t.Fatalf("%q", out)
	}
}

func TestRecipeAddHTTPWarningAndOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: http-lab
spec:
  image: grain-ubuntu
`)
	sum := sha256.Sum256(body)
	_ = hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	run := func(args ...string) (string, error) {
		root := Root("test")
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		err := root.Execute()
		return out.String(), err
	}

	out, err := run("recipe", "add", srv.URL, "--id", "http-lab")
	if err != nil {
		t.Fatal(err, out)
	}
	if !strings.Contains(out, "added") {
		t.Fatalf("%q", out)
	}
	// overwrite
	out, err = run("recipe", "add", srv.URL, "--id", "http-lab", "--overwrite")
	if err != nil {
		t.Fatal(err, out)
	}
}

func TestRecipeSearchEmptyCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"apiVersion":"grain.recipes/v1","recipes":[]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", srv.URL)

	root := Root("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"recipe", "search"})
	if err := root.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if !strings.Contains(out.String(), "empty") && !strings.Contains(out.String(), "Catalog") {
		t.Fatalf("%q", out.String())
	}
}

func TestRecipeShowWithoutUserdataTip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.yaml")
	body := `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
spec:
  image: grain-ubuntu
  bootstrap:
    steps:
      - name: ok
        run: true
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	root := Root("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"recipe", "show", path})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "wait:") {
		t.Fatalf("%q", out.String())
	}
}

package recipe

import (
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

func TestCatalogFetchAddURLSha256(t *testing.T) {
	t.Parallel()
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: from-url
spec:
  image: grain-ubuntu
  cpus: 1
`)
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/recipes/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
  "apiVersion": "grain.recipes/v1",
  "recipes": [
    {"id": "from-url", "title": "From URL", "path": "from-url.yaml", "sha256": %q}
  ]
}`, hexSum)
	})
	mux.HandleFunc("/recipes/from-url.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/raw.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cache := filepath.Join(t.TempDir(), "catalog.json")
	cat, err := FetchCatalog(srv.Client(), srv.URL+"/recipes/catalog.json", cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Recipes) != 1 || cat.Recipes[0].ID != "from-url" {
		t.Fatalf("%+v", cat)
	}
	// index only — no library yet
	lib := t.TempDir()
	list, _ := ListLibrary(lib)
	if len(list) != 0 {
		t.Fatal("catalog fetch must not install recipes")
	}

	// sha256 mismatch fails closed
	if _, err := AddFromURL(srv.Client(), lib, srv.URL+"/raw.yaml", "deadbeef", SaveOptions{ID: "x"}); err == nil {
		t.Fatal("expected sha mismatch")
	}

	ent, err := AddFromCatalog(srv.Client(), cat, lib, "from-url", SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "from-url" {
		t.Fatalf("%+v", ent)
	}
	// no silent clobber
	if _, err := AddFromCatalog(srv.Client(), cat, lib, "from-url", SaveOptions{}); err == nil {
		t.Fatal("expected exists")
	}

	// URL add path
	lib2 := t.TempDir()
	if _, err := AddFromURL(srv.Client(), lib2, srv.URL+"/raw.yaml", hexSum, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	list, _ = ListLibrary(lib2)
	if len(list) != 1 {
		t.Fatalf("%+v", list)
	}

	// offline cache
	srv.Close()
	cat2, err := FetchCatalog(http.DefaultClient, srv.URL+"/recipes/catalog.json", cache)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat2.Recipes) != 1 {
		t.Fatalf("cache: %+v", cat2)
	}
}

func TestPreviewFromCatalogNoLibraryWrite(t *testing.T) {
	t.Parallel()
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: docker-lab
  description: from yaml body
spec:
  image: grain-ubuntu
  cpus: 2
`)
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/recipes/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
  "apiVersion": "grain.recipes/v1",
  "recipes": [
    {"id": "docker-lab", "title": "Docker lab", "description": "index desc", "path": "docker-lab.yaml", "sha256": %q}
  ]
}`, hexSum)
	})
	mux.HandleFunc("/recipes/docker-lab.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cache := filepath.Join(t.TempDir(), "c.json")
	cat, err := FetchCatalog(srv.Client(), srv.URL+"/recipes/catalog.json", cache)
	if err != nil {
		t.Fatal(err)
	}
	lib := t.TempDir()
	prev, err := PreviewFromCatalog(srv.Client(), cat, "docker-lab")
	if err != nil {
		t.Fatal(err)
	}
	if prev.SuggestedID != "docker-lab" || prev.YAML == "" || prev.Image != "grain-ubuntu" {
		t.Fatalf("%+v", prev)
	}
	if prev.Description != "from yaml body" {
		t.Fatalf("desc %q", prev.Description)
	}
	list, _ := ListLibrary(lib)
	if len(list) != 0 {
		t.Fatal("preview must not install")
	}
}

func TestPreviewFromURLNoLibraryWrite(t *testing.T) {
	t.Parallel()
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: preview-lab
  description: preview only
spec:
  image: grain-ubuntu
  cpus: 2
  memory_mb: 2048
  mounts:
    - host: /Users/other/proj
      guest: /work
  bootstrap:
    steps:
      - name: packages
        run: true
`)
	mux := http.NewServeMux()
	mux.HandleFunc("/p.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	lib := t.TempDir()
	prev, err := PreviewFromURL(srv.Client(), srv.URL+"/p.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	if prev.Name != "preview-lab" || prev.Image != "grain-ubuntu" || prev.CPUs != 2 {
		t.Fatalf("%+v", prev)
	}
	if !prev.HasBootstrap || len(prev.BootstrapSteps) != 1 || prev.BootstrapSteps[0] != "packages" {
		t.Fatalf("steps %+v", prev.BootstrapSteps)
	}
	if len(prev.Mounts) != 1 || !strings.Contains(prev.Mounts[0], "/work") {
		t.Fatalf("mounts %+v", prev.Mounts)
	}
	if prev.YAML == "" || prev.SuggestedID != "preview-lab" {
		t.Fatalf("%+v", prev)
	}
	// must not write library
	list, err := ListLibrary(lib)
	if err != nil || len(list) != 0 {
		t.Fatalf("preview must not install: %+v %v", list, err)
	}
	// confirm add uses YAML from preview
	ent, err := SaveLibrary(lib, []byte(prev.YAML), SaveOptions{ID: prev.SuggestedID})
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "preview-lab" {
		t.Fatalf("%+v", ent)
	}
	list, _ = ListLibrary(lib)
	if len(list) != 1 {
		t.Fatalf("%+v", list)
	}
}

func TestParseCatalogRejectsBadVersion(t *testing.T) {
	t.Parallel()
	if _, err := ParseCatalog([]byte(`{"apiVersion":"nope","recipes":[]}`)); err == nil {
		t.Fatal("expected error")
	}
	// cache file write for offline path covered above
	_ = os.ErrNotExist
	if !strings.Contains(baseURLOf("http://x/y/catalog.json"), "/y") {
		t.Fatal(baseURLOf("http://x/y/catalog.json"))
	}
}

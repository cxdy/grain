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
	// empty apiVersion defaults
	c, err := ParseCatalog([]byte(`{"recipes":[]}`))
	if err != nil || c.APIVersion != CatalogAPIVersion {
		t.Fatalf("%+v %v", c, err)
	}
	if _, err := ParseCatalog([]byte(`{`)); err == nil {
		t.Fatal("bad json")
	}
}

func TestCatalogURLAndCachePath(t *testing.T) {
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", "https://example.com/cat.json")
	if CatalogURL() != "https://example.com/cat.json" {
		t.Fatal(CatalogURL())
	}
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", "")
	if CatalogURL() != DefaultCatalogURL {
		t.Fatal(CatalogURL())
	}

	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	got := CatalogCachePath()
	if !strings.Contains(got, filepath.Join(home, "cache", "recipes-catalog.json")) &&
		!strings.HasSuffix(got, filepath.Join("cache", "recipes-catalog.json")) {
		t.Fatalf("cache path %s", got)
	}
	// expandHome ~ in GRAIN_HOME
	t.Setenv("GRAIN_HOME", "~")
	_ = CatalogCachePath()
	t.Setenv("GRAIN_HOME", "")
	_ = CatalogCachePath()
}

func TestResolveEntryURLAndLookup(t *testing.T) {
	t.Parallel()
	var nilCat *Catalog
	if _, err := nilCat.LookupCatalogEntry("x"); err == nil {
		t.Fatal("nil catalog")
	}
	cat := &Catalog{
		BaseURL: "https://cdn.example/recipes",
		Recipes: []CatalogEntry{
			{ID: "a", URL: "https://other/a.yaml"},
			{ID: "b", Path: "https://abs/b.yaml"},
			{ID: "c", Path: "sub/c.yaml"},
			{ID: "d"}, // no path/url
		},
	}
	u, err := cat.ResolveEntryURL(cat.Recipes[0])
	if err != nil || u != "https://other/a.yaml" {
		t.Fatalf("%s %v", u, err)
	}
	u, err = cat.ResolveEntryURL(cat.Recipes[1])
	if err != nil || u != "https://abs/b.yaml" {
		t.Fatalf("%s %v", u, err)
	}
	u, err = cat.ResolveEntryURL(cat.Recipes[2])
	if err != nil || u != "https://cdn.example/recipes/sub/c.yaml" {
		t.Fatalf("%s %v", u, err)
	}
	if _, err := cat.ResolveEntryURL(cat.Recipes[3]); err == nil {
		t.Fatal("no path")
	}
	noBase := &Catalog{Recipes: []CatalogEntry{{ID: "r", Path: "x.yaml"}}}
	if _, err := noBase.ResolveEntryURL(noBase.Recipes[0]); err == nil {
		t.Fatal("relative without base")
	}
	e, err := cat.LookupCatalogEntry("c")
	if err != nil || e.ID != "c" {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := cat.LookupCatalogEntry("missing"); err == nil {
		t.Fatal("missing")
	}
}

func TestFetchCatalogErrors(t *testing.T) {
	t.Parallel()
	// network fail, no cache
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	cache := filepath.Join(t.TempDir(), "missing-cache.json")
	if _, err := FetchCatalog(srv.Client(), srv.URL+"/catalog.json", cache); err == nil {
		t.Fatal("expected fetch error")
	}

	// invalid body after 200
	mux := http.NewServeMux()
	mux.HandleFunc("/bad.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"apiVersion":"wrong"}`))
	})
	srv2 := httptest.NewServer(mux)
	t.Cleanup(srv2.Close)
	if _, err := FetchCatalog(srv2.Client(), srv2.URL+"/bad.json", cache); err == nil {
		t.Fatal("expected parse error")
	}

	// empty catalogURL and cachePath use defaults (still need reachable URL)
	// just exercise the empty-arg branch via a working server + empty cachePath
	good := []byte(`{"apiVersion":"grain.recipes/v1","recipes":[]}`)
	mux3 := http.NewServeMux()
	mux3.HandleFunc("/c.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(good)
	})
	srv3 := httptest.NewServer(mux3)
	t.Cleanup(srv3.Close)
	cp := filepath.Join(t.TempDir(), "c.json")
	cat, err := FetchCatalog(srv3.Client(), srv3.URL+"/c.json", cp)
	if err != nil || cat == nil {
		t.Fatal(err)
	}
	// empty catalogURL falls through to CatalogURL env/default — pass explicit empty then override via env
}

func TestPreviewFromURLValidation(t *testing.T) {
	t.Parallel()
	if _, err := PreviewFromURL(nil, "", ""); err == nil {
		t.Fatal("empty url")
	}
	if _, err := PreviewFromURL(nil, "ftp://x", ""); err == nil {
		t.Fatal("non-http")
	}

	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: ""
spec:
  image: grain-ubuntu
  cpus: 1
  memory_mb: 512
  forwards:
    - guest_port: 8080
    - host_port: 3000
      guest_port: 3000
  userdata: |
    #cloud-config
    runcmd: []
`)
	mux := http.NewServeMux()
	mux.HandleFunc("/r.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	// oversized response
	mux.HandleFunc("/big.yaml", func(w http.ResponseWriter, r *http.Request) {
		// httpGet caps at 3MB; PreviewFromURL checks 2MB after full read.
		// Return just over 2MB of valid-ish bytes.
		big := make([]byte, 2*1024*1024+10)
		copy(big, body)
		_, _ = w.Write(big)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	prev, err := PreviewFromURL(srv.Client(), srv.URL+"/r.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	if prev.SuggestedID == "" || len(prev.Forwards) != 2 {
		t.Fatalf("%+v", prev)
	}
	// :guest and host:guest forms
	if prev.Forwards[0] != ":8080" && prev.Forwards[1] != ":8080" {
		t.Fatalf("forwards %+v", prev.Forwards)
	}
	// bootstrap/userdata warning
	found := false
	for _, w := range prev.Warnings {
		if strings.Contains(w, "bootstrap/userdata") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings %+v", prev.Warnings)
	}

	// sha256: prefix accepted
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	if _, err := PreviewFromURL(srv.Client(), srv.URL+"/r.yaml", "sha256:"+hexSum); err != nil {
		t.Fatal(err)
	}

	if _, err := PreviewFromURL(srv.Client(), srv.URL+"/big.yaml", ""); err == nil {
		t.Fatal("expected too large")
	}
}

func TestPreviewFromCatalogFillsAndAddNil(t *testing.T) {
	t.Parallel()
	// YAML with empty name/description — catalog fills title/desc
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: ""
spec:
  image: grain-ubuntu
  cpus: 1
`)
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])
	mux := http.NewServeMux()
	mux.HandleFunc("/recipes/fill.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cat := &Catalog{
		BaseURL: srv.URL + "/recipes",
		Recipes: []CatalogEntry{
			{ID: "fill-me", Title: "Fill Title", Description: "from index", Path: "fill.yaml", SHA256: hexSum},
		},
	}
	prev, err := PreviewFromCatalog(srv.Client(), cat, "fill-me")
	if err != nil {
		t.Fatal(err)
	}
	if prev.SuggestedID != "fill-me" || prev.Name != "Fill Title" || prev.Description != "from index" {
		t.Fatalf("%+v", prev)
	}
	if _, err := PreviewFromCatalog(srv.Client(), cat, "nope"); err == nil {
		t.Fatal("missing id")
	}
	if _, err := AddFromCatalog(srv.Client(), nil, t.TempDir(), "x", SaveOptions{}); err == nil {
		t.Fatal("nil catalog")
	}
	ent, err := AddFromCatalog(srv.Client(), cat, t.TempDir(), "fill-me", SaveOptions{})
	if err != nil || ent.ID != "fill-me" {
		t.Fatalf("%+v %v", ent, err)
	}
}

func TestBaseURLOfEdge(t *testing.T) {
	t.Parallel()
	if baseURLOf("nopath") != "nopath" {
		t.Fatal(baseURLOf("nopath"))
	}
	if baseURLOf("") != "" {
		t.Fatal("empty")
	}
}

func TestHTTPGetErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)
	if _, err := httpGet(srv.Client(), srv.URL); err == nil || !strings.Contains(err.Error(), "HTTP 418") {
		t.Fatalf("%v", err)
	}
	// connection refused
	if _, err := httpGet(srv.Client(), "http://127.0.0.1:1/"); err == nil {
		t.Fatal("want network error")
	}
}

func TestPreviewFromYAMLMountWarnings(t *testing.T) {
	t.Parallel()
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: mounts
spec:
  image: grain-ubuntu
  cpus: 1
  mounts:
    - host: /absolute/host/path
      guest: /work
    - host: relative
      guest: /rel
`)
	prev, err := PreviewFromYAML(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(prev.Mounts) != 2 {
		t.Fatalf("%+v", prev.Mounts)
	}
	foundAbs := false
	for _, w := range prev.Warnings {
		if strings.Contains(w, "absolute mount") {
			foundAbs = true
		}
	}
	if !foundAbs {
		t.Fatalf("warnings %+v", prev.Warnings)
	}
}

func TestOfflineFetchNoCache(t *testing.T) {
	t.Parallel()
	// closed server + no cache file
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL + "/gone.json"
	srv.Close()
	if _, err := FetchCatalog(http.DefaultClient, url, filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected error")
	}
}

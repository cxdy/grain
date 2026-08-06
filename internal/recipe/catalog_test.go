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

package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestOfficialCatalogInRepo validates recipes/catalog.json against on-disk YAML
// in the monorepo (sha256 + Parse/Compile).
func TestOfficialCatalogInRepo(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	// internal/recipe → repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	catPath := filepath.Join(root, "recipes", "catalog.json")
	b, err := os.ReadFile(catPath)
	if err != nil {
		t.Skip("no recipes/catalog.json:", err)
	}
	var cat Catalog
	if err := json.Unmarshal(b, &cat); err != nil {
		t.Fatal(err)
	}
	if cat.APIVersion != CatalogAPIVersion {
		t.Fatalf("apiVersion %q", cat.APIVersion)
	}
	if len(cat.Recipes) == 0 {
		t.Fatal("empty catalog")
	}
	for _, e := range cat.Recipes {
		p := filepath.Join(root, "recipes", e.Path)
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", e.ID, err)
		}
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		if e.SHA256 != "" && got != e.SHA256 {
			t.Errorf("%s sha256 want %s got %s", e.ID, e.SHA256, got)
		}
		f, err := Parse(body)
		if err != nil {
			t.Errorf("%s parse: %v", e.ID, err)
			continue
		}
		if _, err := f.Compile(); err != nil {
			t.Errorf("%s compile: %v", e.ID, err)
		}
	}
}

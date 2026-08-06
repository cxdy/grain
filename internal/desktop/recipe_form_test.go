package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/recipe"
)

func TestBuildAndSaveRecipeForm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	y, err := BuildRecipeYAML(RecipeForm{
		ID: "my-lab", Name: "my-lab", Description: "test",
		Image: "grain-ubuntu", CPUs: 2, MemoryMB: 2048,
		MountHost: ".", MountGuest: "/work",
		BootstrapRun: "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y, "apiVersion: grain/v1") || !strings.Contains(y, "my-lab") {
		t.Fatal(y)
	}
	f, err := recipe.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Compile(); err != nil {
		t.Fatal(err)
	}

	svc := NewService(Defaults())
	ent, err := svc.SaveRecipeForm(RecipeForm{
		ID: "form-lab", Name: "form-lab", Image: "grain-ubuntu", CPUs: 1, Preset: "docker",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "form-lab" {
		t.Fatalf("%+v", ent)
	}
	p := filepath.Join(home, "recipes", "form-lab.yaml")
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRecipeFormRequiresName(t *testing.T) {
	if _, err := BuildRecipeYAML(RecipeForm{}); err == nil {
		t.Fatal("expected error")
	}
}

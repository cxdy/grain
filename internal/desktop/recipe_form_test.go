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
		BootstrapRun: "echo ok",
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

func TestBuildRecipeYAMLBranches(t *testing.T) {
	// ID only → name from ID; default image; guest port; agent wait
	y, err := BuildRecipeYAML(RecipeForm{ID: "only-id", GuestPort: 8080})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y, "only-id") || !strings.Contains(y, "grain-ubuntu") || !strings.Contains(y, "8080") {
		t.Fatal(y)
	}
	// name only → id from name
	y2, err := BuildRecipeYAML(RecipeForm{Name: "named"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y2, "named") {
		t.Fatal(y2)
	}
	// partial mount error
	if _, err := BuildRecipeYAML(RecipeForm{Name: "m", MountHost: "/tmp"}); err == nil {
		t.Fatal("want mount pair error")
	}
	if _, err := BuildRecipeYAML(RecipeForm{Name: "m", MountGuest: "/g"}); err == nil {
		t.Fatal("want mount pair error")
	}
	// preset without bootstrap → userdata wait
	y3, err := BuildRecipeYAML(RecipeForm{Name: "p", Preset: "docker", Image: "grain-ubuntu"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y3, "docker") {
		t.Fatal(y3)
	}
	// full mount ok
	y4, err := BuildRecipeYAML(RecipeForm{
		Name: "m2", MountHost: ".", MountGuest: "/work", Persistent: true, DiskGB: 10, MemoryMB: 512, CPUs: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y4, "/work") {
		t.Fatal(y4)
	}
}

func TestSaveRecipeFormErrorsAndIDFromName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	svc := NewService(Defaults())
	if _, err := svc.SaveRecipeForm(RecipeForm{}, false); err == nil {
		t.Fatal("want name required")
	}
	ent, err := svc.SaveRecipeForm(RecipeForm{Name: "from-name", Image: "grain-ubuntu"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "from-name" {
		t.Fatalf("%+v", ent)
	}
	// overwrite false on existing → error
	if _, err := svc.SaveRecipeForm(RecipeForm{Name: "from-name", Image: "grain-ubuntu"}, false); err == nil {
		t.Fatal("want exists error")
	}
	if _, err := svc.SaveRecipeForm(RecipeForm{Name: "from-name", Image: "grain-ubuntu"}, true); err != nil {
		t.Fatal(err)
	}
}

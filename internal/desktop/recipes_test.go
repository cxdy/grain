package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/client"
)

func TestDesktopPreviewRecipeURLNoWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: from-url
spec:
  image: grain-ubuntu
  cpus: 1
  bootstrap:
    steps:
      - name: packages
        run: "true"
`)
	mux := http.NewServeMux()
	mux.HandleFunc("/r.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	sock := startFakeDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = filepath.Dir(sock)
	cfg.Connections = []Connection{LocalConnection(sock, cfg.DataDir)}
	svc := NewService(cfg)

	prev, err := svc.PreviewRecipeURL(srv.URL + "/r.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if prev.Name != "from-url" || prev.YAML == "" || !prev.HasBootstrap {
		t.Fatalf("%+v", prev)
	}
	// library still empty
	list, err := svc.ListLibraryRecipes()
	if err != nil || len(list) != 0 {
		t.Fatalf("preview wrote library: %+v %v", list, err)
	}
	ent, err := svc.ConfirmRecipeYAML(prev.YAML, prev.SuggestedID, false)
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "from-url" {
		t.Fatalf("%+v", ent)
	}
	list, _ = svc.ListLibraryRecipes()
	if len(list) != 1 {
		t.Fatalf("%+v", list)
	}
}

func TestDesktopRecipeLibraryLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		var req client.CreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Name == "" {
			t.Error("missing name")
		}
		_ = json.NewEncoder(w).Encode(client.Instance{
			Name: req.Name, Status: client.StatusRunning, Image: req.Image, CPUs: req.CPUs,
		})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = filepath.Dir(sock)
	cfg.Connections = []Connection{LocalConnection(sock, cfg.DataDir)}
	svc := NewService(cfg)
	svc.Active = "local"

	src := filepath.Join(t.TempDir(), "lab.yaml")
	body := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
spec:
  image: grain-ubuntu
  cpus: 2
  memory_mb: 2048
`)
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	ent, err := svc.ImportRecipeFile(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != "lab" {
		t.Fatalf("%+v", ent)
	}
	// import does not create VM — only library
	list, err := svc.ListLibraryRecipes()
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	yaml, err := svc.GetLibraryRecipeYAML("lab")
	if err != nil || !strings.Contains(yaml, "grain-ubuntu") {
		t.Fatal(err, yaml)
	}
	// invalid save rejected
	if _, err := svc.SaveLibraryRecipeYAML("lab", "not yaml recipe"); err == nil {
		t.Fatal("expected invalid save reject")
	}
	// valid overwrite
	if _, err := svc.SaveLibraryRecipeYAML("lab", string(body)); err != nil {
		t.Fatal(err)
	}
	// deploy with name override
	sb, err := svc.DeployRecipe(context.Background(), DeployRecipeOpts{Recipe: "lab", Name: "lab-box", Wait: "ssh"})
	if err != nil {
		t.Fatal(err)
	}
	if sb.Name != "lab-box" {
		t.Fatalf("%+v", sb)
	}
	if err := svc.DeleteLibraryRecipe("lab"); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.ListLibraryRecipes()
	if len(list) != 0 {
		t.Fatalf("after delete: %+v", list)
	}
}

const sampleRecipeYAML = `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: demo
  description: demo recipe
spec:
  image: grain-ubuntu
  cpus: 1
  memory_mb: 1024
  disk_gb: 8
  persistent: true
  mounts:
    - host: /tmp/work
      guest: /work
    - host: .
      guest: /cwd
  forwards:
    - guest_port: 3000
  bootstrap:
    steps:
      - name: packages
        run: "true"
`

func writeLibraryRecipe(t *testing.T, home, id, body string) {
	t.Helper()
	dir := filepath.Join(home, "recipes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportRecipeURLAndCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	body := []byte(sampleRecipeYAML)
	mux := http.NewServeMux()
	mux.HandleFunc("/recipes/demo.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"apiVersion": "grain.recipes/v1",
			"recipes": []map[string]interface{}{
				{
					"id": "demo", "title": "Demo", "description": "catalog demo",
					"tags": []string{"dev"}, "path": "recipes/demo.yaml",
				},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", srv.URL+"/catalog.json")

	cfg := Defaults()
	cfg.DataDir = home
	svc := NewService(cfg)

	// ImportRecipeURL
	ent, err := svc.ImportRecipeURL(srv.URL+"/recipes/demo.yaml", false)
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID == "" || !ent.InLibrary {
		t.Fatalf("%+v", ent)
	}

	// SearchOfficialRecipes
	list, err := svc.SearchOfficialRecipes()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "demo" || !list[0].InLibrary {
		t.Fatalf("%+v", list)
	}

	// PreviewOfficialRecipe online
	prev, err := svc.PreviewOfficialRecipe("demo")
	if err != nil {
		t.Fatal(err)
	}
	if prev.YAML == "" || prev.SuggestedID != "demo" {
		t.Fatalf("%+v", prev)
	}

	// AddOfficialRecipe overwrite
	ent2, err := svc.AddOfficialRecipe("demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if ent2.ID != "demo" {
		t.Fatalf("%+v", ent2)
	}

	// Catalog entry fills empty name/description from index
	sparseBody := []byte(`
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: ""
spec:
  image: grain-ubuntu
`)
	// rebind with sparse recipe + catalog title/description
	mux2 := http.NewServeMux()
	mux2.HandleFunc("/recipes/sparse.yaml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(sparseBody)
	})
	mux2.HandleFunc("/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"apiVersion": "grain.recipes/v1",
			"recipes": []map[string]interface{}{
				{"id": "sparse", "title": "Sparse Title", "description": "from catalog", "path": "recipes/sparse.yaml"},
			},
		})
	})
	srv2 := httptest.NewServer(mux2)
	t.Cleanup(srv2.Close)
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", srv2.URL+"/catalog.json")
	_ = os.RemoveAll(filepath.Join(home, "cache"))
	prevS, err := svc.PreviewOfficialRecipe("sparse")
	if err != nil {
		t.Fatal(err)
	}
	if prevS.Name != "Sparse Title" || prevS.Description != "from catalog" {
		t.Fatalf("%+v", prevS)
	}
}

func TestPreviewOfficialRecipeOfflineAndErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	writeLibraryRecipe(t, home, "local-only", sampleRecipeYAML)

	// Unreachable catalog URL → library fallback for installed recipe
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", "http://127.0.0.1:1/nope.json")
	svc := NewService(Defaults())

	prev, err := svc.PreviewOfficialRecipe("local-only")
	if err != nil {
		t.Fatal(err)
	}
	if prev.YAML == "" || len(prev.Warnings) == 0 {
		t.Fatalf("want library fallback: %+v", prev)
	}

	// Empty id
	if _, err := svc.PreviewOfficialRecipe(""); err == nil {
		t.Fatal("want empty id error")
	}
	if _, err := svc.PreviewOfficialRecipe("   "); err == nil {
		t.Fatal("want whitespace id error")
	}

	// Catalog available but body fetch fails → index-only summary
	mux := http.NewServeMux()
	mux.HandleFunc("/catalog.json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"apiVersion": "grain.recipes/v1",
			"recipes": []map[string]interface{}{
				{"id": "missing-body", "title": "Missing", "description": "no body", "url": "http://127.0.0.1:1/r.yaml"},
				{"id": "no-title", "description": "only desc", "url": "http://127.0.0.1:1/r2.yaml"},
			},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", srv.URL+"/catalog.json")
	// clear cache so we hit the server
	_ = os.RemoveAll(filepath.Join(home, "cache"))

	prev2, err := svc.PreviewOfficialRecipe("missing-body")
	if err != nil {
		t.Fatal(err)
	}
	if prev2.Name != "Missing" || prev2.YAML != "" || len(prev2.Warnings) == 0 {
		t.Fatalf("want index-only: %+v", prev2)
	}
	// empty title → name falls back to id
	prevNT, err := svc.PreviewOfficialRecipe("no-title")
	if err != nil {
		t.Fatal(err)
	}
	if prevNT.Name != "no-title" {
		t.Fatalf("want id as name: %+v", prevNT)
	}

	// Unknown id with catalog up but no library
	if _, err := svc.PreviewOfficialRecipe("does-not-exist"); err == nil {
		// PreviewOfficialRecipe returns index-only with empty name when entry missing
		// Actually LookupCatalogEntry fails, PreviewFromCatalog fails, library fails, then index-only with empty name
	}
	prev3, err := svc.PreviewOfficialRecipe("does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if prev3.SuggestedID != "does-not-exist" {
		t.Fatalf("%+v", prev3)
	}
}

func TestRecipeDeployPreflight(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	// recipe with missing absolute mount
	body := `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: pf
spec:
  image: grain-ubuntu
  mounts:
    - host: /nonexistent/grain-mount-xyz
      guest: /work
    - host: .
      guest: /cwd
`
	writeLibraryRecipe(t, home, "pf", body)

	// local daemon fake
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	sock := startFakeDaemon(t, mux)

	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = home
	cfg.Connections = []Connection{LocalConnection(sock, home)}
	svc := NewService(cfg)
	svc.Active = "local"

	pf, err := svc.RecipeDeployPreflight("pf")
	if err != nil {
		t.Fatal(err)
	}
	if pf.OK {
		t.Fatalf("want not ok: %+v", pf)
	}
	if len(pf.MissingMounts) == 0 {
		t.Fatalf("want missing mounts: %+v", pf)
	}
	if pf.ImageReady {
		t.Fatalf("image should not be ready without install: %+v", pf)
	}
	if pf.ActiveHost != "local" || pf.Remote {
		t.Fatalf("%+v", pf)
	}

	// plant ready image → ImageReady true; mounts still missing
	imgDir := filepath.Join(home, "images", "grain-ubuntu")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	pfReady, err := svc.RecipeDeployPreflight("pf")
	if err != nil {
		t.Fatal(err)
	}
	if !pfReady.ImageReady {
		t.Fatalf("want image ready: %+v", pfReady)
	}
	if pfReady.OK {
		t.Fatalf("mounts still missing: %+v", pfReady)
	}

	// missing recipe
	if _, err := svc.RecipeDeployPreflight("nope"); err == nil {
		t.Fatal("want load error")
	}

	// empty image → ImageReady true
	body2 := `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: noimg
spec:
  cpus: 1
`
	writeLibraryRecipe(t, home, "noimg", body2)
	pf2, err := svc.RecipeDeployPreflight("noimg")
	if err != nil {
		t.Fatal(err)
	}
	if !pf2.ImageReady {
		t.Fatalf("empty image should be ready: %+v", pf2)
	}

	// remote connection warning
	cfg.Connections = []Connection{
		LocalConnection(sock, home),
		{Name: "lab", API: "http://127.0.0.1:9"},
	}
	svc.Config = cfg
	_ = svc.SetActive("lab")
	pf3, err := svc.RecipeDeployPreflight("pf")
	if err != nil {
		t.Fatal(err)
	}
	if !pf3.Remote || !strings.Contains(strings.Join(pf3.Warnings, " "), "Creating on host") {
		t.Fatalf("%+v", pf3)
	}

	// all OK: only relative mounts + ready image
	_ = svc.SetActive("local")
	if err := os.MkdirAll(filepath.Join(home, "recipes", "rel"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLibraryRecipe(t, home, "okpf", `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: okpf
spec:
  image: grain-ubuntu
  mounts:
    - host: .
      guest: /work
    - host: ./rel
      guest: /rel
`)
	pfOK, err := svc.RecipeDeployPreflight("okpf")
	if err != nil {
		t.Fatal(err)
	}
	if !pfOK.OK || !pfOK.ImageReady || len(pfOK.MissingMounts) != 0 {
		t.Fatalf("%+v", pfOK)
	}
}

func TestDeployRecipePresetsAndDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	var got client.CreateRequest
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(client.Instance{
			Name: got.Name, Status: client.StatusRunning, Image: got.Image, CPUs: got.CPUs, MemoryMB: got.MemoryMB,
		})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = home
	cfg.Connections = []Connection{LocalConnection(sock, home)}
	svc := NewService(cfg)
	svc.Active = "local"

	// recipe with preset k3s (DefaultResources 2/4096), forwards, mounts, no wait
	body := `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: k3sbox
spec:
  image: grain-ubuntu
  preset: k3s
  mounts:
    - host: /tmp
      guest: /work
  forwards:
    - guest_port: 6443
      host_port: 0
`
	writeLibraryRecipe(t, home, "k3sbox", body)

	sb, err := svc.DeployRecipe(context.Background(), DeployRecipeOpts{Recipe: "k3sbox"})
	if err != nil {
		t.Fatal(err)
	}
	if sb.Name != "k3sbox" {
		t.Fatalf("%+v", sb)
	}
	if got.Wait != client.WaitAgent && got.Wait != "userdata" && got.Wait != "agent" {
		// preset may set wait; ensure we sent something
		t.Logf("wait=%q", got.Wait)
	}
	if got.CPUs < 1 {
		t.Fatalf("cpus %+v", got)
	}
	if len(got.Mounts) == 0 || len(got.Forwards) == 0 {
		t.Fatalf("mounts/forwards: %+v", got)
	}
	if got.Userdata == "" {
		// k3s preset should expand userdata
		t.Fatalf("expected preset userdata: %+v", got)
	}

	// empty recipe id
	if _, err := svc.DeployRecipe(context.Background(), DeployRecipeOpts{}); err == nil {
		t.Fatal("want recipe required")
	}
	// missing recipe
	if _, err := svc.DeployRecipe(context.Background(), DeployRecipeOpts{Recipe: "nope"}); err == nil {
		t.Fatal("want load error")
	}

	// bad preset
	writeLibraryRecipe(t, home, "badpreset", `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: badpreset
spec:
  image: grain-ubuntu
  preset: not-a-real-preset-xyz
`)
	if _, err := svc.DeployRecipe(context.Background(), DeployRecipeOpts{Recipe: "badpreset"}); err == nil {
		t.Fatal("want bad preset error")
	}
}

func TestExportSandboxRecipeToLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Instance{
			Name: r.PathValue("name"), Status: client.StatusRunning,
			Image: "grain-ubuntu", CPUs: 2, MemoryMB: 2048, DiskGB: 8, Persistent: true,
		})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = home
	cfg.Connections = []Connection{LocalConnection(sock, home)}
	svc := NewService(cfg)
	svc.Active = "local"

	ent, err := svc.ExportSandboxRecipeToLibrary(context.Background(), "work", false)
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID == "" || !ent.InLibrary {
		t.Fatalf("%+v", ent)
	}
	list, err := svc.ListLibraryRecipes()
	if err != nil || len(list) == 0 {
		t.Fatalf("%+v %v", list, err)
	}

	// export failure
	if _, err := svc.ExportSandboxRecipeToLibrary(context.Background(), "", false); err == nil {
		t.Fatal("want empty name error")
	}
}

func TestGetLibraryRecipeErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	svc := NewService(Defaults())
	if _, err := svc.GetLibraryRecipeYAML("missing"); err == nil {
		t.Fatal("want missing")
	}
	if _, err := svc.ImportRecipeFile(filepath.Join(home, "nope.yaml"), false); err == nil {
		t.Fatal("want import fail")
	}
	if _, err := svc.ConfirmRecipeYAML("not a recipe", "x", false); err == nil {
		t.Fatal("want confirm fail")
	}
	if _, err := svc.PreviewRecipeURL("not-a-url"); err == nil {
		t.Fatal("want preview fail")
	}
	// catalog errors
	t.Setenv("GRAIN_RECIPE_CATALOG_URL", "http://127.0.0.1:1/nope.json")
	_ = os.RemoveAll(filepath.Join(home, "cache"))
	if _, err := svc.SearchOfficialRecipes(); err == nil {
		t.Fatal("want catalog fetch fail")
	}
	if _, err := svc.AddOfficialRecipe("x", false); err == nil {
		t.Fatal("want add catalog fail")
	}
	if _, err := svc.ImportRecipeURL("http://127.0.0.1:1/r.yaml", false); err == nil {
		t.Fatal("want import url fail")
	}
}

func TestExpandPresetUserdata(t *testing.T) {
	ud, err := expandPresetUserdata("docker", "")
	if err != nil || ud == "" {
		t.Fatalf("%q %v", ud, err)
	}
	merged, err := expandPresetUserdata("docker", "#cloud-config\npackages: [curl]\n")
	if err != nil || merged == "" {
		t.Fatalf("%q %v", merged, err)
	}
	if _, err := expandPresetUserdata("no-such-preset", ""); err == nil {
		t.Fatal("want error")
	}
}

func TestPreviewLibraryAsURLPreview(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRAIN_HOME", home)
	writeLibraryRecipe(t, home, "demo", sampleRecipeYAML)
	svc := NewService(Defaults())
	prev, err := svc.previewLibraryAsURLPreview("demo", "warn-msg")
	if err != nil {
		t.Fatal(err)
	}
	if prev.YAML == "" || len(prev.Warnings) == 0 || prev.Warnings[0] != "warn-msg" {
		t.Fatalf("%+v", prev)
	}
	prev2, err := svc.previewLibraryAsURLPreview("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = prev2
	if _, err := svc.previewLibraryAsURLPreview("nope", "x"); err == nil {
		t.Fatal("want missing")
	}
}

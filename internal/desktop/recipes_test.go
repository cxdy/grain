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
        run: true
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

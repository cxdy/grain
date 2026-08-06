package desktop

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/recipe"
)

// startFakeDaemon serves a minimal grain API on a unix socket and returns the path.
// Uses a short path under os.TempDir — macOS sundial path limits break long t.TempDir sockets.
func startFakeDaemon(t *testing.T, handler http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gd-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "g.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = ln.Close()
	})
	return sock
}

func TestServiceHealthListLifecycle(t *testing.T) {
	var creates atomic.Int32
	var deletes atomic.Int32
	var stops atomic.Int32
	var starts atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{
			{Name: "dev", Status: client.StatusRunning, Image: "grain-ubuntu", CPUs: 2, MemoryMB: 2048},
		})
	})
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		creates.Add(1)
		var req client.CreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Image == "" {
			t.Error("expected image defaulted by service before POST or in body")
		}
		inst := client.Instance{
			Name: req.Name, Status: client.StatusRunning, Image: req.Image,
			Persistent: req.Persistent, CPUs: req.CPUs, MemoryMB: req.MemoryMB,
		}
		if inst.Name == "" {
			inst.Name = "auto-1"
		}
		_ = json.NewEncoder(w).Encode(inst)
	})
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		starts.Add(1)
		name := r.PathValue("name")
		_ = json.NewEncoder(w).Encode(client.Instance{Name: name, Status: client.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/shutdown", func(w http.ResponseWriter, r *http.Request) {
		stops.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		deletes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})

	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = filepath.Dir(sock)
	cfg.Connections = []Connection{LocalConnection(sock, cfg.DataDir)}
	svc := NewService(cfg)
	svc.Active = "local"

	ctx := context.Background()
	hs, err := svc.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hs.Healthy || !hs.Local {
		t.Fatalf("health: %+v", hs)
	}

	list, err := svc.ListSandboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "dev" || list[0].Status != "running" {
		t.Fatalf("list: %+v", list)
	}

	created, err := svc.CreateSandbox(ctx, CreateOpts{Name: "box", Image: "grain-ubuntu", CPUs: 2})
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "box" || creates.Load() != 1 {
		t.Fatalf("create: %+v count=%d", created, creates.Load())
	}

	st, err := svc.StartSandbox(ctx, "box")
	if err != nil || st.Name != "box" || starts.Load() != 1 {
		t.Fatalf("start: %+v %v count=%d", st, err, starts.Load())
	}
	if err := svc.StopSandbox(ctx, "box"); err != nil || stops.Load() != 1 {
		t.Fatalf("stop: %v count=%d", err, stops.Load())
	}
	if err := svc.RemoveSandbox(ctx, "box"); err != nil || deletes.Load() != 1 {
		t.Fatalf("rm: %v count=%d", err, deletes.Load())
	}
	got, err := svc.GetSandbox(ctx, "box")
	if err != nil || got.Name != "box" {
		t.Fatalf("get: %+v %v", got, err)
	}
}

func TestExportSandboxRecipe(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Instance{
			Name:       r.PathValue("name"),
			Status:     client.StatusRunning,
			Image:      "grain-ubuntu",
			CPUs:       4,
			MemoryMB:   8192,
			DiskGB:     32,
			Persistent: true,
			Mounts:     []client.Mount{{Host: "/tmp/src", Guest: "/work"}},
			Forwards:   []client.PortForward{{GuestPort: 3000}},
		})
	})

	sock := startFakeDaemon(t, mux)
	dataDir := t.TempDir()
	vmDir := filepath.Join(dataDir, "vms", "work")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]interface{}{
		"name": "work", "cpus": 4, "memory_mb": 8192, "disk_gb": 32,
		"image": "grain-ubuntu", "persistent": true,
		"arch": "arm64", "network": "overlay", "gpu": "virtio",
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = dataDir
	cfg.Connections = []Connection{LocalConnection(sock, dataDir)}
	svc := NewService(cfg)
	svc.Active = "local"

	y, err := svc.ExportSandboxRecipe(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y, "name: work") || !strings.Contains(y, "image: grain-ubuntu") {
		t.Fatal(y)
	}
	if !strings.Contains(y, "guest: /work") || !strings.Contains(y, "guest_port: 3000") {
		t.Fatal(y)
	}
	if !strings.Contains(y, "arch: arm64") || !strings.Contains(y, "network: overlay") {
		t.Fatal(y)
	}
	if !strings.Contains(y, "persistent: true") {
		t.Fatal(y)
	}
	// Must parse as a valid recipe.
	if _, err := recipe.Parse([]byte(y)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExportSandboxRecipe(context.Background(), ""); err == nil {
		t.Fatal("expected empty name error")
	}
}

func TestServiceRemoteHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "auth", 401)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{{Name: "r1", Status: client.StatusStopped}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	cfg := Defaults()
	cfg.Connections = []Connection{
		LocalConnection(cfg.Socket, cfg.DataDir),
		{Name: "lab", API: ts.URL, Token: "tok"},
	}
	svc := NewService(cfg)
	if err := svc.SetActive("lab"); err != nil {
		t.Fatal(err)
	}
	hs, err := svc.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hs.Healthy || hs.Local {
		t.Fatalf("%+v", hs)
	}
	list, err := svc.ListSandboxes(context.Background())
	if err != nil || len(list) != 1 || list[0].Name != "r1" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestServiceEnsureReadyStartsLocal(t *testing.T) {
	var healthy atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			http.Error(w, "no", 503)
			return
		}
		w.WriteHeader(200)
	})
	sock := startFakeDaemon(t, mux)

	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = filepath.Dir(sock)
	svc := NewService(cfg)
	r := &fakeRunner{path: "/bin/grain"}
	svc.Runner = r
	svc.Sleep = func(time.Duration) {}
	svc.HealthWait = 2 * time.Second
	svc.HealthPoll = time.Millisecond

	// After grain up is "started", flip healthy
	orig := r
	svc.Runner = &hookRunner{fakeRunner: orig, onStart: func() { healthy.Store(true) }}

	res, hs, err := svc.EnsureReady(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Started || !hs.Healthy {
		t.Fatalf("res=%+v hs=%+v", res, hs)
	}
}

type hookRunner struct {
	*fakeRunner
	onStart func()
}

func (h *hookRunner) StartBackground(ctx context.Context, name string, args ...string) error {
	err := h.fakeRunner.StartBackground(ctx, name, args...)
	if h.onStart != nil {
		h.onStart()
	}
	return err
}

func TestServiceValidation(t *testing.T) {
	svc := NewService(Defaults())
	// No dialable socket and unreachable API — must report unhealthy (not panic).
	svc.Config.Socket = filepath.Join(t.TempDir(), "missing.sock")
	svc.Config.API = "127.0.0.1:1"
	hs, err := svc.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hs.Healthy {
		t.Fatal("expected unhealthy")
	}
	if _, err := svc.StartSandbox(context.Background(), ""); err == nil {
		t.Fatal("empty name")
	}
	if err := svc.StopSandbox(context.Background(), ""); err == nil {
		t.Fatal("empty stop")
	}
	if err := svc.RemoveSandbox(context.Background(), ""); err == nil {
		t.Fatal("empty rm")
	}
	if err := svc.SetActive("nope"); err == nil {
		t.Fatal("bad active")
	}
}

func TestServiceCreateDefaults(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		var req client.CreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Image != "grain-ubuntu" {
			t.Errorf("image %q", req.Image)
		}
		if req.CPUs != 2 {
			t.Errorf("cpus %d", req.CPUs)
		}
		_ = json.NewEncoder(w).Encode(client.Instance{Name: "x", Status: client.StatusRunning, Image: req.Image})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.Image = "grain-ubuntu"
	cfg.DefaultCPUs = 2
	svc := NewService(cfg)
	_, err := svc.CreateSandbox(context.Background(), CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstanceToSandboxNil(t *testing.T) {
	t.Parallel()
	if instanceToSandbox(nil).Name != "" {
		t.Fatal("nil")
	}
}

func TestServiceConnections(t *testing.T) {
	svc := NewService(Defaults())
	conns := svc.Connections()
	if len(conns) < 1 {
		t.Fatal("want local")
	}
}

func TestServiceEnsureReadyAlreadyHealthy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	svc := NewService(cfg)
	r := &fakeRunner{path: "/bin/grain"}
	svc.Runner = r
	svc.Sleep = func(time.Duration) {}
	res, hs, err := svc.EnsureReady(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyHealthy || res.Started || !hs.Healthy {
		t.Fatalf("res=%+v hs=%+v", res, hs)
	}
	if r.started.Load() != 0 {
		t.Fatal("should not start grain")
	}
}

func TestServiceEnsureReadyRemoteUnhealthy(t *testing.T) {
	cfg := Defaults()
	cfg.Connections = []Connection{
		LocalConnection(cfg.Socket, cfg.DataDir),
		{Name: "lab", API: "http://127.0.0.1:1"},
	}
	svc := NewService(cfg)
	_ = svc.SetActive("lab")
	svc.Runner = &fakeRunner{}
	svc.Sleep = func(time.Duration) {}
	svc.HealthWait = 10 * time.Millisecond
	_, _, err := svc.EnsureReady(context.Background())
	if err == nil {
		t.Fatal("want remote unhealthy error")
	}
}

func TestServiceShellAndLogs(t *testing.T) {
	dir := t.TempDir()
	vmDir := filepath.Join(dir, "vms", "dev")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "serial.log"), []byte("hello log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "missing.sock")
	cfg.API = "127.0.0.1:17474"
	cfg.Connections = []Connection{LocalConnection(cfg.Socket, dir)}
	svc := NewService(cfg)

	info, err := svc.ShellSession("dev", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	// missing sock → TCP shell path
	if info.UseUnix || info.Cols != 100 {
		t.Fatalf("%+v", info)
	}
	if !strings.Contains(info.URL, "17474") {
		t.Fatalf("url %q", info.URL)
	}
	logs, err := svc.ReadSandboxLogs("dev", LogSerial, 0)
	if err != nil || logs.Content != "hello log\n" {
		t.Fatalf("%+v %v", logs, err)
	}

	// remote cannot read logs
	cfg.Connections = append(cfg.Connections, Connection{Name: "lab", API: "http://127.0.0.1:9"})
	svc.Config = cfg
	_ = svc.SetActive("lab")
	if _, err := svc.ReadSandboxLogs("dev", LogSerial, 0); err == nil {
		t.Fatal("want remote logs error")
	}
}

func TestServiceTCPFallbackList(t *testing.T) {
	// Real user setup: TCP API + token, no unix socket.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer labtok" {
			http.Error(w, "auth", 401)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{
			{Name: "work", Status: client.StatusRunning, Image: "grain-ubuntu", CPUs: 4, MemoryMB: 8192},
		})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	cfg := Defaults()
	cfg.Socket = filepath.Join(t.TempDir(), "nope.sock")
	cfg.API = "0.0.0.0:" + port
	cfg.APIToken = "labtok"
	svc := NewService(cfg)
	svc.Sleep = func(time.Duration) {}
	svc.HealthWait = time.Second

	res, hs, err := svc.EnsureReady(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hs.Healthy || !res.AlreadyHealthy {
		t.Fatalf("res=%+v hs=%+v", res, hs)
	}
	list, err := svc.ListSandboxes(context.Background())
	if err != nil || len(list) != 1 || list[0].Name != "work" {
		t.Fatalf("%+v %v", list, err)
	}
	sum := svc.Summary("/tmp/cfg.yaml")
	if !sum.HasToken || sum.DialHint == "" {
		t.Fatalf("summary %+v", sum)
	}
}

func TestBuildCreateRequest(t *testing.T) {
	t.Parallel()
	cfg := Config{Image: "img", DefaultCPUs: 3, DefaultMemoryMB: 1024, DefaultDiskGB: 10}
	req, err := buildCreateRequest(CreateOpts{}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if req.Image != "img" || req.CPUs != 3 || req.MemoryMB != 1024 || req.DiskGB != 10 || req.Wait != client.WaitAuto {
		t.Fatalf("%+v", req)
	}
	req, err = buildCreateRequest(CreateOpts{
		Image: "x", CPUs: 8, MemoryMB: 1, DiskGB: 2, Wait: "agent",
		Publish: "8080:80", Mounts: "/tmp/h:/work", Arch: "arm64", GPU: "virtio",
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if req.Image != "x" || req.CPUs != 8 || req.Wait != "agent" || req.Arch != "arm64" {
		t.Fatalf("%+v", req)
	}
	if len(req.Forwards) != 1 || req.Forwards[0].GuestPort != 80 {
		t.Fatalf("forwards %+v", req.Forwards)
	}
	if len(req.Mounts) != 1 || req.Mounts[0].Guest != "/work" {
		t.Fatalf("mounts %+v", req.Mounts)
	}
}

func TestValidateSandboxName(t *testing.T) {
	t.Parallel()
	if err := ValidateSandboxName("Test"); err == nil {
		t.Fatal("uppercase invalid")
	}
	if err := ValidateSandboxName("test"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSandboxName(""); err != nil {
		t.Fatal(err)
	}
}

func TestListSkipsNilInstances(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		// encode with null entry
		_, _ = w.Write([]byte(`[{"name":"a","status":"running"},null]`))
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	svc := NewService(cfg)
	list, err := svc.ListSandboxes(context.Background())
	if err != nil || len(list) != 1 || list[0].Name != "a" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestExecRunnerLookPath(t *testing.T) {
	var r ExecRunner
	p, err := r.LookPath("go")
	if err != nil || p == "" {
		t.Fatalf("%q %v", p, err)
	}
}

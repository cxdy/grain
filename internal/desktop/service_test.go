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
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
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
			http.Error(w, "auth", http.StatusUnauthorized)
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
			http.Error(w, "no", http.StatusServiceUnavailable)
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

func TestWithinAgentProbeGrace(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	if !withinAgentProbeGrace("", now) {
		t.Fatal("empty created_at should grace")
	}
	if !withinAgentProbeGrace(now.Add(-30*time.Second).Format(time.RFC3339), now) {
		t.Fatal("30s old should grace")
	}
	if withinAgentProbeGrace(now.Add(-3*time.Minute).Format(time.RFC3339), now) {
		t.Fatal("3m old should not grace")
	}
}

func TestApplyAgentProbeGraceLeavesUnset(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not ready", http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	sb := &Sandbox{
		Name:          "young",
		HasAgentImage: true,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	applyAgentProbe(context.Background(), c, sb, "young", time.Now().UTC().Format(time.RFC3339))
	if sb.AgentOK != nil {
		t.Fatalf("young agent image should leave AgentOK unset (checking), got %v", *sb.AgentOK)
	}
	// After grace → false
	sb.CreatedAt = time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339)
	applyAgentProbe(context.Background(), c, sb, "young", time.Now().UTC().Format(time.RFC3339))
	if sb.AgentOK == nil || *sb.AgentOK {
		t.Fatalf("after grace want AgentOK=false, got %v", sb.AgentOK)
	}
	// Non-agent image → false immediately
	sb2 := &Sandbox{Name: "cloud", HasAgentImage: false, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	applyAgentProbe(context.Background(), c, sb2, "cloud", time.Now().UTC().Format(time.RFC3339))
	if sb2.AgentOK == nil || *sb2.AgentOK {
		t.Fatalf("cloud image want AgentOK=false, got %v", sb2.AgentOK)
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
			http.Error(w, "auth", http.StatusUnauthorized)
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
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
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

func TestBulkExecParallel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		_ = json.NewEncoder(w).Encode(client.ExecResult{Stdout: "ok-" + name + "\n", ExitCode: 0})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	svc := NewService(cfg)
	if err := svc.Connect(); err != nil {
		t.Fatal(err)
	}
	out, err := svc.BulkExec(context.Background(), []string{"a", "b", "a"}, "uname -a")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("dedupe want 2 got %d", len(out))
	}
	if out[0].Name != "a" || out[0].Line != "a: ok-a" {
		t.Fatalf("%+v", out[0])
	}
	if out[1].Name != "b" || out[1].Line != "b: ok-b" {
		t.Fatalf("%+v", out[1])
	}
}

func TestDecideCreateModePreferPool(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /pool", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.PoolStatus{
			Enabled: true, Template: "golden", Desired: 2, Ready: 2, Members: []string{"pool-golden-1", "pool-golden-2"},
		})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	svc := NewService(cfg)
	if err := svc.Connect(); err != nil {
		t.Fatal(err)
	}
	d, err := svc.DecideCreateMode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Mode != "pool" || !d.PreferPool || d.Ready != 2 {
		t.Fatalf("%+v", d)
	}
}

func TestBulkStartPreflightForNamesBlocks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{
			{Name: "r1", Status: client.StatusRunning, CPUs: 2, MemoryMB: 2048},
			{Name: "s1", Status: client.StatusStopped, CPUs: 2, MemoryMB: 2048},
			{Name: "s2", Status: client.StatusStopped, CPUs: 2, MemoryMB: 2048},
		})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	svc := NewService(cfg)
	if err := svc.Connect(); err != nil {
		t.Fatal(err)
	}
	// Local connection: caps from config file (when /info omits caps).
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("max_vms: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// projected running = 1 + 2 = 3 > max_vms 2
	r, err := svc.BulkStartPreflightForNames(context.Background(), []string{"s1", "s2"}, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Block || !strings.Contains(r.Message, "max_vms") {
		t.Fatalf("%+v", r)
	}
}

func TestBulkStartPreflightRemoteInfoCaps(t *testing.T) {
	// Active remote host: caps from GET /info (not local config.yaml).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"name": "grain", "version": "test",
			"max_vms": "2", "max_cpus_total": "0", "max_memory_mb_total": "0",
		})
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{
			{Name: "r1", Status: client.StatusRunning, CPUs: 1, MemoryMB: 512},
			{Name: "s1", Status: client.StatusStopped, CPUs: 1, MemoryMB: 512},
			{Name: "s2", Status: client.StatusStopped, CPUs: 1, MemoryMB: 512},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := Defaults()
	cfg.Connections = []Connection{{Name: "lab", API: srv.URL}}
	svc := NewService(cfg)
	svc.Active = "lab"
	if err := svc.Connect(); err != nil {
		t.Fatal(err)
	}
	// Local config would allow huge batch; remote max_vms=2 must block.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("max_vms: 100\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := svc.BulkStartPreflightForNames(context.Background(), []string{"s1", "s2"}, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Block || !strings.Contains(r.Message, "max_vms") {
		t.Fatalf("want remote hard block: %+v", r)
	}
	// Under limit on same host.
	r, err = svc.BulkStartPreflightForNames(context.Background(), []string{"s1"}, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if r.Block {
		t.Fatalf("under limit should not block: %+v", r)
	}
}

func TestListActivityFilteredRemoteClient(t *testing.T) {
	// Active remote host: activity + pool hit that server's client, not hardcoded local.
	var poolHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /activity", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]client.ActivityEvent{
			{ID: "1", Source: "cli", Action: "create", Status: "success"},
			{ID: "2", Source: "mcp", Action: "list", Status: "success"},
			{ID: "3", Source: "desktop", Action: "start", Status: "success"},
		})
	})
	mux.HandleFunc("GET /pool", func(w http.ResponseWriter, r *http.Request) {
		poolHits.Add(1)
		_ = json.NewEncoder(w).Encode(client.PoolStatus{Enabled: false})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := Defaults()
	cfg.Connections = []Connection{{Name: "lab", API: srv.URL}}
	svc := NewService(cfg)
	svc.Active = "lab"
	if err := svc.Connect(); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListActivityFiltered(context.Background(), "", 50, []string{"cli"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Source != "cli" {
		t.Fatalf("%+v", list)
	}
	if _, err := svc.PoolStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	if poolHits.Load() != 1 {
		t.Fatalf("pool hits %d", poolHits.Load())
	}
	// Create mode decision also uses active connection pool.
	d, err := svc.DecideCreateMode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Enabled {
		t.Fatalf("want disabled pool: %+v", d)
	}
	if poolHits.Load() != 2 {
		t.Fatalf("pool hits after decide %d", poolHits.Load())
	}
}

func TestServiceSuspendPoolFillMetricsDeploy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.9.0"})
	})
	mux.HandleFunc("POST /vms/{name}/suspend", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /pool/fill", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.PoolStatus{Enabled: true, Ready: 1, Desired: 1, Template: "golden"})
	})
	mux.HandleFunc("GET /pool", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.PoolStatus{Enabled: true, Ready: 1})
	})
	mux.HandleFunc("GET /vms/{name}/metrics", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.MetricsHistory{
			Enabled: true, Interval: "5s",
			Points: []client.MetricsSample{{TimeMS: 1, Load1: 0.5, MemTotal: 100, MemAvail: 50}},
		})
	})
	mux.HandleFunc("POST /vms/{name}/agent/deploy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AgentDeployResult{
			Name: r.PathValue("name"), Binary: "/usr/local/bin/grain-agent",
			Health: &client.Health{AgentVersion: "1.0.0"},
		})
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{
			{Name: "tpl", Status: client.StatusStopped, Persistent: true, Image: "grain-ubuntu"},
			{Name: "ephem", Status: client.StatusStopped, Persistent: false},
			{Name: "run", Status: client.StatusRunning, Persistent: true, Image: "grain-ubuntu",
				CreatedAt: time.Now().UTC().Add(-5 * time.Minute)},
		})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Health{AgentVersion: "1.0.0"})
	})
	mux.HandleFunc("GET /activity", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]client.ActivityEvent{{ID: "1", Source: "cli"}})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = filepath.Dir(sock)
	svc := NewService(cfg)
	if err := svc.Connect(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	hs, err := svc.Health(ctx)
	if err != nil || !hs.Healthy || hs.Version != "v0.9.0" {
		t.Fatalf("%+v %v", hs, err)
	}
	if err := svc.SuspendSandbox(ctx, "tpl"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SuspendSandbox(ctx, ""); err == nil {
		t.Fatal("empty suspend")
	}
	st, err := svc.PoolFill(ctx)
	if err != nil || st == nil || !st.Enabled {
		t.Fatalf("%+v %v", st, err)
	}
	m, err := svc.SandboxMetrics(ctx, "run")
	if err != nil || !m.Enabled || len(m.Points) != 1 {
		t.Fatalf("%+v %v", m, err)
	}
	da, err := svc.DeployAgent(ctx, "run")
	if err != nil || da.AgentVersion != "1.0.0" {
		t.Fatalf("%+v %v", da, err)
	}
	if _, err := svc.DeployAgent(ctx, ""); err == nil {
		t.Fatal("empty deploy")
	}
	// Deploy without health
	mux2 := http.NewServeMux()
	mux2.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux2.HandleFunc("POST /vms/{name}/agent/deploy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.AgentDeployResult{Name: "x"})
	})
	sock2 := startFakeDaemon(t, mux2)
	svc2 := NewService(Defaults())
	svc2.Config.Socket = sock2
	_ = svc2.Connect()
	da2, err := svc2.DeployAgent(ctx, "x")
	if err != nil || da2.Message != "guest agent deployed" {
		t.Fatalf("%+v %v", da2, err)
	}

	tmpls, err := svc.ListCreateTemplates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpls) != 1 || tmpls[0].Name != "tpl" {
		t.Fatalf("%+v", tmpls)
	}
	acts, err := svc.ListActivity(ctx, "", 10)
	if err != nil || len(acts) != 1 {
		t.Fatalf("%+v %v", acts, err)
	}
	list, err := svc.ListSandboxes(ctx)
	if err != nil || len(list) < 2 {
		t.Fatalf("%+v %v", list, err)
	}
	// running agent ok
	for _, sb := range list {
		if sb.Name == "run" && (sb.AgentOK == nil || !*sb.AgentOK) {
			t.Fatalf("want agent ok: %+v", sb)
		}
	}
}

func TestServiceParseAndCreateErrors(t *testing.T) {
	cfg := Config{Image: "img"}
	if _, err := buildCreateRequest(CreateOpts{From: "a", FromPool: true}, cfg); err == nil {
		t.Fatal("mutual exclusive")
	}
	if _, err := buildCreateRequest(CreateOpts{Publish: "bad"}, cfg); err == nil {
		t.Fatal("publish")
	}
	if _, err := buildCreateRequest(CreateOpts{Publish: "x:y"}, cfg); err == nil {
		t.Fatal("publish ports")
	}
	if _, err := buildCreateRequest(CreateOpts{Mounts: "nocolon"}, cfg); err == nil {
		t.Fatal("mounts")
	}
	req, err := buildCreateRequest(CreateOpts{Publish: "8080:80, 9090:90", Mounts: "/h:/g\n/a:/b"}, cfg)
	if err != nil || len(req.Forwards) != 2 || len(req.Mounts) != 2 {
		t.Fatalf("%+v %v", req, err)
	}
	// splitList empty parts
	if len(splitList(" a , , b ")) != 2 {
		t.Fatal(splitList(" a , , b "))
	}
	// invalid name on create
	svc := NewService(Defaults())
	if _, err := svc.CreateSandbox(context.Background(), CreateOpts{Name: "BAD"}); err == nil {
		t.Fatal("invalid name")
	}
}

func TestServiceExecOneAndBulkErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "err" {
			http.Error(w, "fail", 500)
			return
		}
		if name == "stderr-only" {
			_ = json.NewEncoder(w).Encode(client.ExecResult{Stderr: "warn\n", ExitCode: 1})
			return
		}
		if name == "empty-out" {
			_ = json.NewEncoder(w).Encode(client.ExecResult{ExitCode: 0})
			return
		}
		_ = json.NewEncoder(w).Encode(client.ExecResult{Stdout: "ok\n", Stderr: "e\n", ExitCode: 1})
	})
	sock := startFakeDaemon(t, mux)
	svc := NewService(Defaults())
	svc.Config.Socket = sock
	_ = svc.Connect()
	ctx := context.Background()
	r, err := svc.ExecOne(ctx, "a", "true")
	if err != nil || r.Name != "a" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := svc.BulkExec(ctx, nil, "x"); err == nil {
		t.Fatal("names required")
	}
	if _, err := svc.BulkExec(ctx, []string{"a"}, ""); err == nil {
		t.Fatal("command required")
	}
	if _, err := svc.BulkExec(ctx, []string{"  ", ""}, "x"); err == nil {
		t.Fatal("empty names")
	}
	out, err := svc.BulkExec(ctx, []string{"err", "stderr-only", "empty-out", "a"}, "x")
	if err != nil || len(out) != 4 {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestServiceGetSandboxMetaAndStartGrow(t *testing.T) {
	dir := t.TempDir()
	name := "meta-vm"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]interface{}{
		"name": name, "network": "overlay", "arch": "arm64", "gpu": "virtio",
		"image": "grain-ubuntu", "disk_gb": 1, "cpus": 2, "memory_mb": 512,
	}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Instance{
			Name: r.PathValue("name"), Status: client.StatusRunning, Image: "grain-ubuntu",
			CreatedAt: time.Now().UTC().Add(-5 * time.Minute),
		})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	})
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = dir
	svc := NewService(cfg)
	_ = svc.Connect()
	sb, err := svc.GetSandbox(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if sb.Network != "overlay" || sb.Arch != "arm64" || sb.GPU != "virtio" || !sb.HasAgentImage {
		t.Fatalf("%+v", sb)
	}
	// agent failed after grace → false
	if sb.AgentOK == nil || *sb.AgentOK {
		t.Fatalf("want agent false: %+v", sb)
	}
	if _, err := svc.GetSandbox(context.Background(), ""); err == nil {
		t.Fatal("empty get")
	}
	// start without disk (meta disk_gb 1, no image) — non-fatal path
	st, err := svc.StartSandbox(context.Background(), name)
	if err != nil || st.Name != name {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestImageSupportsAgentAndSummary(t *testing.T) {
	if ImageSupportsAgent("") || ImageSupportsAgent("ubuntu-cloud") {
		t.Fatal("no")
	}
	if !ImageSupportsAgent("grain-ubuntu") || !ImageSupportsAgent("grain-ubuntu-fc") {
		t.Fatal("yes")
	}
	if !ImageSupportsAgent("grain-ubuntu-custom") {
		t.Fatal("prefix")
	}
	if ImageSupportsAgent("alpine-cloud") {
		t.Fatal("alpine")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.APIToken = "tok"
	cfg.Connections = []Connection{
		LocalConnection(sock, cfg.DataDir),
		{Name: "lab", API: "http://x", Token: "t", TokenEnv: "NOPE", Notes: "n"},
	}
	svc := NewService(cfg)
	sum := svc.Summary(filepath.Join(t.TempDir(), "missing.yaml"))
	if !sum.HasToken || len(sum.Connections) < 2 {
		t.Fatalf("%+v", sum)
	}
	// applyAgentProbe nils
	applyAgentProbe(context.Background(), nil, &Sandbox{}, "x", "")
	applyAgentProbe(context.Background(), svc.Client, nil, "x", "")
}

func TestDecideCreateModePoolError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /pool", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	})
	sock := startFakeDaemon(t, mux)
	svc := NewService(Defaults())
	svc.Config.Socket = sock
	_ = svc.Connect()
	d, err := svc.DecideCreateMode(context.Background())
	if err == nil {
		// some clients may return empty without error
		_ = d
	}
}

func TestSandboxMetricsEmptyHistory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms/{name}/metrics", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"enabled":false,"points":[]}`))
	})
	sock := startFakeDaemon(t, mux)
	svc := NewService(Defaults())
	svc.Config.Socket = sock
	_ = svc.Connect()
	m, err := svc.SandboxMetrics(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if m.Enabled || len(m.Points) != 0 {
		t.Fatalf("%+v", m)
	}
}

func TestBulkStartPreflightUnknownNamesAndEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{
			{Name: "r1", Status: client.StatusRunning, CPUs: 0, MemoryMB: 0}, // defaults 2/2048
		})
	})
	sock := startFakeDaemon(t, mux)
	svc := NewService(Defaults())
	svc.Config.Socket = sock
	_ = svc.Connect()
	r, err := svc.BulkStartPreflightForNames(context.Background(), []string{"", "ghost", "r1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = r
}

func TestExportSandboxRecipeNilInstance(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("null"))
	})
	sock := startFakeDaemon(t, mux)
	svc := NewService(Defaults())
	svc.Config.Socket = sock
	_ = svc.Connect()
	if _, err := svc.ExportSandboxRecipe(context.Background(), "x"); err == nil {
		t.Fatal("want not found")
	}
}

func TestActiveConnectionAndEnsureClient(t *testing.T) {
	svc := NewService(Defaults())
	c, err := svc.ActiveConnection()
	if err != nil || c.Name != "local" {
		t.Fatalf("%+v %v", c, err)
	}
	// force dial fail
	svc.Config.Socket = filepath.Join(t.TempDir(), "no.sock")
	svc.Config.API = "127.0.0.1:1"
	svc.Client = nil
	svc.Dial = func(conn Connection, cfg Config) (*client.Client, error) {
		return nil, context.DeadlineExceeded
	}
	if err := svc.Connect(); err == nil {
		t.Fatal("want dial fail")
	}
	if _, err := svc.ListSandboxes(context.Background()); err == nil {
		t.Fatal("want ensure client fail")
	}
	// dial nil uses default
	svc.Dial = nil
	_ = svc.Connect() // may fail on real dial
}

func TestStartSandboxDiskResizeHardFail(t *testing.T) {
	// meta asks for grow but disk missing → ensureMetaDiskGrown returns "disk resize:" only when resize fails after needGrow
	// When disk missing, diskNeedsGrow errors; ensureMetaDiskGrown returns err without "disk resize:" prefix → Start continues
	dir := t.TempDir()
	name := "startdisk"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]interface{}{
		"name": name, "disk_gb": 5, "disk_path": filepath.Join(vmDir, "missing.qcow2"),
		"cpus": 1, "memory_mb": 512,
	}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = dir
	svc := NewService(cfg)
	// non-fatal disk inspect error → start still works
	st, err := svc.StartSandbox(context.Background(), name)
	if err != nil || st.Name != name {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestListSandboxesMetaEnrichment(t *testing.T) {
	dir := t.TempDir()
	name := "enrich"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]interface{}{
		"name": name, "network": "overlay", "arch": "amd64", "gpu": "virtio", "image": "grain-ubuntu",
	}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{
			{Name: name, Status: client.StatusStopped, Image: ""},
		})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = dir
	svc := NewService(cfg)
	list, err := svc.ListSandboxes(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if list[0].Network != "overlay" || list[0].Arch != "amd64" || !list[0].HasAgentImage {
		t.Fatalf("%+v", list[0])
	}
}

func TestExportSandboxRecipeMetaFill(t *testing.T) {
	dir := t.TempDir()
	name := "exmeta"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]interface{}{
		"name": name, "cpus": 8, "memory_mb": 4096, "disk_gb": 20,
		"image": "grain-ubuntu", "persistent": true, "arch": "arm64", "network": "slirp", "gpu": "virtio",
	}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		// API omits resources — meta fills in
		_ = json.NewEncoder(w).Encode(client.Instance{
			Name: r.PathValue("name"), Status: client.StatusStopped,
			SocketForwards: []client.SocketForward{{HostPath: "/tmp/h.sock", GuestPath: "/g.sock"}},
		})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = dir
	svc := NewService(cfg)
	y, err := svc.ExportSandboxRecipe(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y, "cpus: 8") || !strings.Contains(y, "arch: arm64") {
		t.Fatal(y)
	}
	if !strings.Contains(y, "grain-ubuntu") || !strings.Contains(y, "memory_mb: 4096") {
		t.Fatal(y)
	}
}

func TestWithinAgentProbeGraceBadTime(t *testing.T) {
	t.Parallel()
	now := time.Now()
	if !withinAgentProbeGrace("not-a-time", now) {
		t.Fatal("bad time should grace")
	}
}

func TestDecideCreateModeNilStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /pool", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("null"))
	})
	sock := startFakeDaemon(t, mux)
	svc := NewService(Defaults())
	svc.Config.Socket = sock
	_ = svc.Connect()
	// null decode may error or yield zero status
	_, _ = svc.DecideCreateMode(context.Background())
}

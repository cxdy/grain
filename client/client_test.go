package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/client"
)

// mockDaemon is a minimal httptest-backed stand-in for the grain API surface.
func mockDaemon(t *testing.T, token string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"name": "grain", "version": "test"})
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, []*client.Instance{
			{Name: "demo", Status: client.StatusRunning, CPUs: 2},
		})
	})
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		// Stream path: echo wait mode in NDJSON phases for tests.
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(200)
			enc := json.NewEncoder(w)
			wait := r.URL.Query().Get("wait")
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseQEMU, Message: "qemu"})
			switch wait {
			case client.WaitAgent:
				_ = enc.Encode(client.CreateEvent{Phase: client.PhaseWaitAgent, Message: "wait agent"})
			case client.WaitUserdata:
				_ = enc.Encode(client.CreateEvent{Phase: client.PhaseWaitAgent, Message: "wait agent"})
				_ = enc.Encode(client.CreateEvent{Phase: "wait_userdata", Message: "wait userdata"})
			case client.WaitBootstrap:
				_ = enc.Encode(client.CreateEvent{Phase: client.PhaseWaitAgent, Message: "wait agent"})
				_ = enc.Encode(client.CreateEvent{Phase: client.PhaseBootstrap, Message: "wait bootstrap"})
			default:
				_ = enc.Encode(client.CreateEvent{Phase: client.PhaseWaitSSH, Message: "wait ssh"})
			}
			var req client.CreateRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			name := req.Name
			if name == "" {
				name = "auto"
			}
			inst := &client.Instance{Name: name, Status: client.StatusRunning, CPUs: 2}
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseReady, Name: name, Instance: inst})
			return
		}
		var req client.CreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		name := req.Name
		if name == "" {
			name = "auto"
		}
		// Expose wait/timeout in tags so tests can assert query params.
		tags := map[string]string{}
		if wq := r.URL.Query().Get("wait"); wq != "" {
			tags["wait"] = wq
		}
		if tq := r.URL.Query().Get("timeout"); tq != "" {
			tags["timeout"] = tq
		}
		writeJSON(w, http.StatusCreated, &client.Instance{
			Name:   name,
			Status: client.StatusRunning,
			CPUs:   2,
			Tags:   tags,
		})
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, &client.Instance{
			Name:   r.PathValue("name"),
			Status: client.StatusRunning,
		})
	})
	mux.HandleFunc("DELETE /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"message": "deleted", "name": r.PathValue("name")})
	})
	mux.HandleFunc("POST /vms/{name}/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"message": "shutdown", "name": r.PathValue("name")})
	})
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/pause", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"message": "paused", "name": r.PathValue("name")})
	})
	mux.HandleFunc("POST /vms/{name}/resume", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"message": "running", "name": r.PathValue("name")})
	})
	mux.HandleFunc("POST /vms/{name}/suspend", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"message": "suspended", "name": r.PathValue("name")})
	})
	mux.HandleFunc("POST /vms/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/forwards", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		var body struct {
			HostPort  int `json:"host_port"`
			GuestPort int `json:"guest_port"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.GuestPort == 0 {
			writeJSON(w, 400, map[string]string{"error": "guest_port is required"})
			return
		}
		hp := body.HostPort
		if hp == 0 {
			hp = 18080
		}
		writeJSON(w, http.StatusCreated, &client.LiveForward{HostPort: hp, GuestPort: body.GuestPort, PID: 99})
	})
	mux.HandleFunc("DELETE /vms/{name}/forwards/{hostPort}", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"message": "removed"})
	})
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		cmd := r.URL.Query().Get("cmd")
		if cmd == "" {
			writeJSON(w, 400, map[string]string{"error": "cmd is required"})
			return
		}
		args := r.URL.Query()["args"]
		out := cmd
		if len(args) > 0 {
			out = strings.Join(args, " ")
		}
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(200)
			enc := json.NewEncoder(w)
			_ = enc.Encode(client.ExecFrame{Type: "started", PID: 1})
			_ = enc.Encode(client.ExecFrame{Type: "stdout", Data: out + "\n"})
			code := 0
			_ = enc.Encode(client.ExecFrame{Type: "exit", ExitCode: &code})
			return
		}
		writeJSON(w, 200, &client.ExecResult{Stdout: out + "\n", ExitCode: 0})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, &client.Health{Hostname: "guest", AgentVersion: "0.2.0"})
	})
	mux.HandleFunc("GET /vms/{name}/stats", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, &client.Stats{UptimeSec: 1.5, MemTotal: 1024, MemAvail: 512, Load1: 0.1})
	})
	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, []client.SecretMeta{{Name: "tok", Size: 3, Mode: "0600"}})
	})
	mux.HandleFunc("POST /secrets", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		var body client.SecretPut
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Name == "" {
			writeJSON(w, 400, map[string]string{"error": "name is required"})
			return
		}
		writeJSON(w, http.StatusCreated, &client.SecretMeta{Name: body.Name, Size: 3, Mode: body.Mode})
	})
	mux.HandleFunc("DELETE /secrets/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, map[string]string{"message": "deleted", "name": r.PathValue("name")})
	})
	mux.HandleFunc("POST /vms/{name}/secrets/{secretName}", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		path := "/run/grain/secrets/" + r.PathValue("secretName")
		if r.Body != nil {
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) == nil && body["path"] != "" {
				path = body["path"]
			}
		}
		writeJSON(w, 200, map[string]string{"path": path, "mode": "0600"})
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		if r.URL.Query().Get("path") == "" {
			writeJSON(w, 400, map[string]string{"error": "path is required"})
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		if r.URL.Query().Get("path") == "" {
			writeJSON(w, 400, map[string]string{"error": "path is required"})
			return
		}
		mode := r.URL.Query().Get("mode")
		if mode == "tar" {
			w.Header().Set("Content-Type", "application/x-tar")
			_, _ = w.Write([]byte("tar-bytes"))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("file-bytes"))
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, []client.FSInfo{{Name: "a.txt", Type: "file", Size: 1, Mode: "0644"}})
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		writeJSON(w, 200, &client.FSInfo{Name: "a.txt", Type: "file", Size: 1, Mode: "0644"})
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		if !checkAuth(w, r, token) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return httptest.NewServer(mux)
}

func checkAuth(w http.ResponseWriter, r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	want := "Bearer " + token
	if r.Header.Get("Authorization") != want {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func TestDialHTTPBasicLifecycle(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
	info, err := c.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info["name"] != "grain" {
		t.Fatalf("info %+v", info)
	}

	inst, err := c.Create(ctx, client.CreateRequest{Name: "box", Persistent: false})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if inst.Name != "box" {
		t.Fatalf("name %q", inst.Name)
	}

	got, err := c.Get(ctx, "box")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "box" {
		t.Fatalf("get %q", got.Name)
	}

	list, err := c.List(ctx)
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v %v", err, list)
	}

	if err := c.Shutdown(ctx, "box"); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if _, err := c.Start(ctx, "box"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Pause(ctx, "box"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := c.Resume(ctx, "box"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := c.Suspend(ctx, "box"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if _, err := c.Restore(ctx, "box"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	lf, err := c.AddForward(ctx, "box", 0, 8080)
	if err != nil {
		t.Fatalf("AddForward: %v", err)
	}
	if lf.HostPort == 0 || lf.GuestPort != 8080 {
		t.Fatalf("AddForward result %+v", lf)
	}
	if err := c.RemoveForward(ctx, "box", lf.HostPort); err != nil {
		t.Fatalf("RemoveForward: %v", err)
	}
	if err := c.Delete(ctx, "box"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestCreateWaitAndTimeoutQuery(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	inst, err := c.Create(ctx, client.CreateRequest{
		Name:    "waitbox",
		Wait:    client.WaitAgent,
		Timeout: "45s",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if inst.Tags["wait"] != client.WaitAgent {
		t.Fatalf("wait query not sent: tags=%v", inst.Tags)
	}
	if inst.Tags["timeout"] != "45s" {
		t.Fatalf("timeout query not sent: tags=%v", inst.Tags)
	}

	var phases []string
	streamInst, err := c.CreateStream(ctx, client.CreateRequest{
		Name: "stream-agent",
		Wait: client.WaitAgent,
	}, func(ev client.CreateEvent) {
		phases = append(phases, ev.Phase)
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	if streamInst.Name != "stream-agent" {
		t.Fatalf("stream name %q", streamInst.Name)
	}
	joined := strings.Join(phases, ",")
	if !strings.Contains(joined, client.PhaseWaitAgent) {
		t.Fatalf("want wait_agent in phases %v", phases)
	}
	if !strings.Contains(joined, client.PhaseReady) {
		t.Fatalf("want ready in phases %v", phases)
	}
}


func TestWaitModeConstants(t *testing.T) {
	t.Parallel()
	if client.WaitAuto != "auto" || client.WaitSSH != "ssh" || client.WaitAgent != "agent" ||
		client.WaitUserdata != "userdata" || client.WaitBootstrap != "bootstrap" {
		t.Fatalf("wait constants: auto=%q ssh=%q agent=%q userdata=%q bootstrap=%q",
			client.WaitAuto, client.WaitSSH, client.WaitAgent, client.WaitUserdata, client.WaitBootstrap)
	}
	if client.PhaseBootstrap != "bootstrap" || client.PhaseUserdata != "userdata" {
		t.Fatalf("phase constants: bootstrap=%q userdata=%q", client.PhaseBootstrap, client.PhaseUserdata)
	}
}

func TestCreateWaitBootstrapQuery(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	inst, err := c.Create(ctx, client.CreateRequest{
		Name:    "bootbox",
		Wait:    client.WaitBootstrap,
		Timeout: "2m",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if inst.Tags["wait"] != client.WaitBootstrap {
		t.Fatalf("wait query not sent: tags=%v", inst.Tags)
	}
	if inst.Tags["timeout"] != "2m" {
		t.Fatalf("timeout query not sent: tags=%v", inst.Tags)
	}

	var phases []string
	streamInst, err := c.CreateStream(ctx, client.CreateRequest{
		Name: "stream-bootstrap",
		Wait: client.WaitBootstrap,
	}, func(ev client.CreateEvent) {
		phases = append(phases, ev.Phase)
	})
	if err != nil {
		t.Fatalf("CreateStream: %v", err)
	}
	if streamInst.Name != "stream-bootstrap" {
		t.Fatalf("stream name %q", streamInst.Name)
	}
	joined := strings.Join(phases, ",")
	if !strings.Contains(joined, client.PhaseBootstrap) {
		t.Fatalf("want bootstrap in phases %v", phases)
	}
	if !strings.Contains(joined, client.PhaseReady) {
		t.Fatalf("want ready in phases %v", phases)
	}
}

func TestDialHTTPWithToken(t *testing.T) {
	t.Parallel()
	const tok = "secret-token"
	ts := mockDaemon(t, tok)
	t.Cleanup(ts.Close)

	// No token → 401 on protected routes
	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Health(ctx); err != nil {
		t.Fatalf("health should be open: %v", err)
	}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("expected unauthorized without token")
	}

	// With token → OK
	c2, err := client.DialHTTP(ts.URL, tok)
	if err != nil {
		t.Fatal(err)
	}
	list, err := c2.List(ctx)
	if err != nil {
		t.Fatalf("List with token: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len %d", len(list))
	}

	res, err := c2.Exec(ctx, "demo", "echo", "hi")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if strings.TrimSpace(res.Stdout) != "hi" {
		t.Fatalf("stdout %q", res.Stdout)
	}

	h, err := c2.AgentHealth(ctx, "demo")
	if err != nil || h.AgentVersion == "" {
		t.Fatalf("AgentHealth: %v %+v", err, h)
	}

	var stdout strings.Builder
	code, err := c2.ExecStream(ctx, "demo", client.ExecOpts{Cmd: "echo", Args: []string{"streamed"}}, func(f client.ExecFrame) error {
		if f.Type == "stdout" {
			stdout.WriteString(f.Data)
		}
		return nil
	})
	if err != nil || code != 0 {
		t.Fatalf("ExecStream: code=%d err=%v", code, err)
	}
	if strings.TrimSpace(stdout.String()) != "streamed" {
		t.Fatalf("stream stdout %q", stdout.String())
	}

	if err := c2.PutFile(ctx, "demo", "/tmp/x", strings.NewReader("data"), 4, client.CPOpts{Mode: "0644"}); err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	var buf strings.Builder
	if err := c2.GetFile(ctx, "demo", "/tmp/x", &buf); err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if buf.String() != "file-bytes" {
		t.Fatalf("GetFile %q", buf.String())
	}

	entries, err := c2.ReadDir(ctx, "demo", "/tmp")
	if err != nil || len(entries) == 0 {
		t.Fatalf("ReadDir: %v %v", err, entries)
	}
	st, err := c2.Stat(ctx, "demo", "/tmp/a.txt")
	if err != nil || st.Type != "file" {
		t.Fatalf("Stat: %v %+v", err, st)
	}
	if err := c2.Mkdir(ctx, "demo", "/tmp/n", true, "0755"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := c2.Remove(ctx, "demo", "/tmp/n", true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestDialUnix(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := filepath.Join(dir, "grain.sock")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, []*client.Instance{})
	})

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		_ = os.Remove(sock)
	})

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	c, err := client.DialUnix(sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
	list, err := c.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil {
		t.Fatal("nil list")
	}
}

func TestDialHTTPRequiresBase(t *testing.T) {
	t.Parallel()
	if _, err := client.DialHTTP("", ""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := client.DialUnix(""); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetTokenTokenBase(t *testing.T) {
	t.Parallel()
	c, err := client.DialHTTP("http://127.0.0.1:7474/", "initial")
	if err != nil {
		t.Fatal(err)
	}
	if c.Token() != "initial" {
		t.Fatalf("token %q", c.Token())
	}
	if c.Base() != "http://127.0.0.1:7474" {
		t.Fatalf("base %q (trailing slash should be trimmed)", c.Base())
	}
	c.SetToken("next")
	if c.Token() != "next" {
		t.Fatalf("after SetToken: %q", c.Token())
	}
	c.SetToken("")
	if c.Token() != "" {
		t.Fatal("expected empty token")
	}
}

func TestStopAndStatsAndSecrets(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.Stop(ctx, "demo"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	st, err := c.Stats(ctx, "demo")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.MemTotal == 0 {
		t.Fatalf("stats %+v", st)
	}

	list, err := c.ListSecrets(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSecrets: %v %v", err, list)
	}
	meta, err := c.SetSecret(ctx, client.SecretPut{Name: "k", DataBase64: "YWJj", Mode: "0600"})
	if err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if meta.Name != "k" {
		t.Fatalf("meta %+v", meta)
	}
	out, err := c.InjectSecret(ctx, "demo", "k", "/tmp/secret")
	if err != nil {
		t.Fatalf("InjectSecret: %v", err)
	}
	if out["path"] != "/tmp/secret" {
		t.Fatalf("inject path %v", out)
	}
	out2, err := c.InjectSecret(ctx, "demo", "k", "")
	if err != nil {
		t.Fatalf("InjectSecret default: %v", err)
	}
	if out2["path"] == "" {
		t.Fatal("expected default path")
	}
	if err := c.DeleteSecret(ctx, "k"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
}

func TestPutTarGetTar(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.PutTar(ctx, "demo", "/tmp/out", strings.NewReader("tar-payload")); err != nil {
		t.Fatalf("PutTar: %v", err)
	}
	var buf strings.Builder
	if err := c.GetTar(ctx, "demo", "/tmp/out", &buf); err != nil {
		t.Fatalf("GetTar: %v", err)
	}
	if buf.String() != "tar-bytes" {
		t.Fatalf("GetTar body %q", buf.String())
	}

	if err := c.PutTar(ctx, "demo", "", strings.NewReader("x")); err == nil {
		t.Fatal("expected empty path error for PutTar")
	}
	if err := c.GetTar(ctx, "demo", "", &buf); err == nil {
		t.Fatal("expected empty path error for GetTar")
	}
	if err := c.PutFile(ctx, "demo", "", strings.NewReader("x"), 1, client.CPOpts{}); err == nil {
		t.Fatal("expected empty path error for PutFile")
	}
	if err := c.GetFile(ctx, "demo", "", &buf); err == nil {
		t.Fatal("expected empty path error for GetFile")
	}
	if _, err := c.ReadDir(ctx, "demo", ""); err == nil {
		t.Fatal("expected empty path error for ReadDir")
	}
	if _, err := c.Stat(ctx, "demo", ""); err == nil {
		t.Fatal("expected empty path error for Stat")
	}
	if err := c.Mkdir(ctx, "demo", "", true, "0755"); err == nil {
		t.Fatal("expected empty path error for Mkdir")
	}
	if err := c.Remove(ctx, "demo", "", false); err == nil {
		t.Fatal("expected empty path error for Remove")
	}
}

func TestCreateStreamErrorAndReadyNameOnly(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") != "1" {
			writeJSON(w, 400, map[string]string{"error": "need stream"})
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		enc := json.NewEncoder(w)
		switch r.URL.Query().Get("wait") {
		case "err":
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseError, Error: "boom"})
		case "err-msg":
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseError, Message: "msg-only"})
		case "err-empty":
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseError})
		case "name-only":
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseReady, Name: "solo", SSHPort: 22})
		default:
			// no ready event
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseQEMU, Message: "qemu"})
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := c.CreateStream(ctx, client.CreateRequest{Wait: "err"}, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("want boom error, got %v", err)
	}
	if _, err := c.CreateStream(ctx, client.CreateRequest{Wait: "err-msg"}, nil); err == nil || !strings.Contains(err.Error(), "msg-only") {
		t.Fatalf("want msg-only, got %v", err)
	}
	if _, err := c.CreateStream(ctx, client.CreateRequest{Wait: "err-empty"}, nil); err == nil || !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("want create failed, got %v", err)
	}
	inst, err := c.CreateStream(ctx, client.CreateRequest{Wait: "name-only"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "solo" || inst.SSHPort != 22 {
		t.Fatalf("inst %+v", inst)
	}
	if _, err := c.CreateStream(ctx, client.CreateRequest{}, nil); err == nil {
		t.Fatal("expected missing ready event")
	}
}

func TestExecValidationAndStreamOpts(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := c.Exec(ctx, "demo", ""); err == nil {
		t.Fatal("expected empty cmd error")
	}
	if _, err := c.ExecStream(ctx, "demo", client.ExecOpts{}, nil); err == nil {
		t.Fatal("expected empty cmd error")
	}
	if _, err := c.ExecStream(ctx, "demo", client.ExecOpts{Cmd: "true"}, nil); err == nil {
		t.Fatal("expected onFrame required")
	}

	uid := uint32(1000)
	gid := uint32(1000)
	code, err := c.ExecStream(ctx, "demo", client.ExecOpts{
		Cmd:  "echo",
		Args: []string{"ok"},
		UID:  &uid,
		GID:  &gid,
		Cwd:  "/tmp",
	}, func(client.ExecFrame) error { return nil })
	if err != nil || code != 0 {
		t.Fatalf("ExecStream opts: code=%d err=%v", code, err)
	}
}

func TestHealthUnhealthyAndAPIErrors(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 500, map[string]string{"error": "list boom"})
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 500, map[string]string{})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Health(ctx); err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("Health: %v", err)
	}
	if _, err := c.List(ctx); err == nil || !strings.Contains(err.Error(), "list boom") {
		t.Fatalf("List: %v", err)
	}
	if _, err := c.Info(ctx); err == nil {
		t.Fatal("expected Info error")
	}
}

func TestDialUnixToken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sock := filepath.Join(dir, "grain.sock")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer unix-tok" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		writeJSON(w, 200, []*client.Instance{})
	})

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	c, err := client.DialUnixToken(sock, "unix-tok")
	if err != nil {
		t.Fatal(err)
	}
	if c.Token() != "unix-tok" {
		t.Fatalf("token %q", c.Token())
	}
	if _, err := c.List(context.Background()); err != nil {
		t.Fatalf("List: %v", err)
	}
}

func TestPutFileWithUIDGID(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	uid := uint32(1)
	gid := uint32(2)
	if err := c.PutFile(context.Background(), "demo", "/tmp/x", strings.NewReader("ab"), 2, client.CPOpts{
		UID:  &uid,
		GID:  &gid,
		Mode: "0644",
	}); err != nil {
		t.Fatalf("PutFile: %v", err)
	}
}

func TestExecStreamErrorFrame(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(200)
			enc := json.NewEncoder(w)
			_ = enc.Encode(client.ExecFrame{Type: "error", Error: "agent dead"})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "exec failed"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err = c.ExecStream(ctx, "demo", client.ExecOpts{Cmd: "true"}, func(client.ExecFrame) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "agent dead") {
		t.Fatalf("want agent dead, got %v", err)
	}
	if _, err := c.Exec(ctx, "demo", "true"); err == nil || !strings.Contains(err.Error(), "exec failed") {
		t.Fatalf("want exec failed, got %v", err)
	}
}

func TestCreateStreamHTTPError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 400, map[string]string{"error": "bad create"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(context.Background(), client.CreateRequest{Name: "x"}); err == nil {
		t.Fatal("expected create error")
	}
	if _, err := c.CreateStream(context.Background(), client.CreateRequest{Name: "x"}, nil); err == nil {
		t.Fatal("expected stream create error")
	}
}

func TestSetTokenAppliedToRequests(t *testing.T) {
	t.Parallel()
	const tok = "late-token"
	ts := mockDaemon(t, tok)
	t.Cleanup(ts.Close)

	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("expected unauthorized")
	}
	c.SetToken(tok)
	if _, err := c.List(context.Background()); err != nil {
		t.Fatalf("List after SetToken: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Merged from coverage_boost_test.go
// ---------------------------------------------------------------------------

func TestClientFullSuccessSurface(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"name": "grain", "version": "t"})
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		ok(w, []*client.Instance{{Name: "a", Status: client.StatusRunning}})
	})
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			enc := json.NewEncoder(w)
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseQEMU})
			_ = enc.Encode(client.CreateEvent{
				Phase:    client.PhaseReady,
				Name:     "n",
				Instance: &client.Instance{Name: "n", Status: client.StatusRunning},
			})
			return
		}
		w.WriteHeader(http.StatusCreated)
		ok(w, &client.Instance{Name: "n", Status: client.StatusRunning})
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("DELETE /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"message": "deleted"})
	})
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/shutdown", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"message": "ok"})
	})
	mux.HandleFunc("POST /vms/{name}/pause", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/resume", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/suspend", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/forwards", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		ok(w, &client.LiveForward{HostPort: 9, GuestPort: 80, PID: 1})
	})
	mux.HandleFunc("DELETE /vms/{name}/forwards/{port}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, "\n")
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
			_, _ = io.WriteString(w, `{"type":"stdout","data":"hi"}`+"\n")
			_, _ = io.WriteString(w, `{"type":"stderr","data":"e"}`+"\n")
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
			return
		}
		ok(w, &client.ExecResult{Stdout: "hi", ExitCode: 0})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Health{Hostname: "g", AgentVersion: "1"})
	})
	mux.HandleFunc("GET /vms/{name}/stats", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Stats{MemTotal: 1, UptimeSec: 1})
	})
	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, r *http.Request) {
		ok(w, []client.SecretMeta{{Name: "k"}})
	})
	mux.HandleFunc("POST /secrets", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		ok(w, &client.SecretMeta{Name: "k"})
	})
	mux.HandleFunc("DELETE /secrets/{name}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/secrets/{s}", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"path": "/p"})
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "tar" {
			_, _ = w.Write([]byte("tar"))
			return
		}
		_, _ = w.Write([]byte("bin"))
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		ok(w, []client.FSInfo{{Name: "a", Type: "file", Size: 1, Mode: "0644"}})
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.FSInfo{Name: "a", Type: "file", Size: 1, Mode: "0644"})
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Info(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, client.CreateRequest{Name: "n", Wait: "ssh", Timeout: "30s"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if _, err := c.CreateStream(ctx, client.CreateRequest{Name: "n"}, func(ev client.CreateEvent) { n++ }); err != nil || n < 1 {
		t.Fatalf("stream n=%d err=%v", n, err)
	}
	if _, err := c.Get(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Pause(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Resume(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Suspend(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Restore(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddForward(ctx, "n", 0, 80); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveForward(ctx, "n", 9); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, "n", "true", "a"); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(1), uint32(2)
	code, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "true", UID: &uid, GID: &gid, Cwd: "/tmp"}, func(f client.ExecFrame) error {
		return nil
	})
	if err != nil || code != 0 {
		t.Fatalf("%d %v", code, err)
	}
	if _, err := c.AgentHealth(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stats(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListSecrets(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetSecret(ctx, client.SecretPut{Name: "k", DataBase64: "YQ=="}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteSecret(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(ctx, "n", "k", "/p"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(ctx, "n", "k", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.PutFile(ctx, "n", "/a", strings.NewReader("z"), 1, client.CPOpts{UID: &uid, GID: &gid, Mode: "0644"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, "n", "/a", &buf); err != nil || buf.String() != "bin" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := c.PutTar(ctx, "n", "/a", strings.NewReader("t")); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := c.GetTar(ctx, "n", "/a", &buf); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadDir(ctx, "n", "/"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stat(ctx, "n", "/a"); err != nil {
		t.Fatal(err)
	}
	if err := c.Mkdir(ctx, "n", "/d", true, "0755"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, "n", "/d", true); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, "n", "/d", false); err != nil {
		t.Fatal(err)
	}
	if c.Base() == "" || c.Token() != "tok" {
		t.Fatalf("base/token %q %q", c.Base(), c.Token())
	}
}

func TestClientDecodeAPIErrorNoJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "plain")
	}))
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(context.Background()); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("%v", err)
	}
}

func TestClientExecNonJSONErrorBody(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = io.WriteString(w, "bad gateway")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(context.Background(), "n", "true"); err == nil {
		t.Fatal("expected error")
	}
}

func TestClientExecStreamNoExitAndBadFrame(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		switch r.URL.Query().Get("cmd") {
		case "badjson":
			_, _ = io.WriteString(w, "{not-json}\n")
		case "noexit":
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
		case "emptyerr":
			_, _ = io.WriteString(w, `{"type":"error"}`+"\n")
		case "onframe":
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
		default:
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "badjson"}, func(client.ExecFrame) error { return nil }); err == nil {
		t.Fatal("badjson")
	}
	if _, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "noexit"}, func(client.ExecFrame) error { return nil }); err == nil {
		t.Fatal("noexit")
	}
	if _, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "emptyerr"}, func(client.ExecFrame) error { return nil }); err == nil {
		t.Fatal("emptyerr")
	}
	_, err = c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "onframe"}, func(client.ExecFrame) error {
		return io.EOF
	})
	if err == nil {
		t.Fatal("onframe")
	}
}

func TestClientCreateStreamBlankLines(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, "\n\n")
		_ = json.NewEncoder(w).Encode(client.CreateEvent{
			Phase: client.PhaseReady,
			Name:  "solo",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := c.CreateStream(context.Background(), client.CreateRequest{}, nil)
	if err != nil || inst.Name != "solo" {
		t.Fatalf("%+v %v", inst, err)
	}
}

func TestDialHTTPTrimSlashAndEmpty(t *testing.T) {
	t.Parallel()
	if _, err := client.DialHTTP("", ""); err == nil {
		t.Fatal("empty base")
	}
	if _, err := client.DialUnix(""); err == nil {
		t.Fatal("empty sock")
	}
	c, err := client.DialHTTP("http://example.com/", "t")
	if err != nil {
		t.Fatal(err)
	}
	if c.Base() != "http://example.com" {
		t.Fatalf("%q", c.Base())
	}
	if c.Token() != "t" {
		t.Fatalf("%q", c.Token())
	}
}

// ---------------------------------------------------------------------------
// Merged from error_paths_test.go
// ---------------------------------------------------------------------------

// errServer returns configurable status/body for every request.
func errServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func TestClientErrorPathsAllMethods(t *testing.T) {
	t.Parallel()
	// Non-2xx JSON error
	srv := errServer(t, 500, `{"error":"boom"}`)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.Health(ctx); err == nil {
		t.Fatal("health")
	}
	if _, err := c.Info(ctx); err == nil {
		t.Fatal("info")
	}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("list")
	}
	if _, err := c.Create(ctx, client.CreateRequest{Name: "x"}); err == nil {
		t.Fatal("create")
	}
	if _, err := c.Get(ctx, "x"); err == nil {
		t.Fatal("get")
	}
	if err := c.Delete(ctx, "x"); err == nil {
		t.Fatal("delete")
	}
	if _, err := c.Start(ctx, "x"); err == nil {
		t.Fatal("start")
	}
	if err := c.Stop(ctx, "x"); err == nil {
		t.Fatal("stop")
	}
	if err := c.Shutdown(ctx, "x"); err == nil {
		t.Fatal("shutdown")
	}
	if err := c.Pause(ctx, "x"); err == nil {
		t.Fatal("pause")
	}
	if err := c.Resume(ctx, "x"); err == nil {
		t.Fatal("resume")
	}
	if err := c.Suspend(ctx, "x"); err == nil {
		t.Fatal("suspend")
	}
	if _, err := c.Restore(ctx, "x"); err == nil {
		t.Fatal("restore")
	}
	if _, err := c.AddForward(ctx, "x", 0, 80); err == nil {
		t.Fatal("addfwd")
	}
	if err := c.RemoveForward(ctx, "x", 8080); err == nil {
		t.Fatal("rmfwd")
	}
	if _, err := c.Exec(ctx, "x", "true"); err == nil {
		t.Fatal("exec")
	}
	if _, err := c.AgentHealth(ctx, "x"); err == nil {
		t.Fatal("agent health")
	}
	if _, err := c.Stats(ctx, "x"); err == nil {
		t.Fatal("stats")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("list secrets")
	}
	if _, err := c.SetSecret(ctx, client.SecretPut{Name: "k", DataBase64: "dg=="}); err == nil {
		t.Fatal("set secret")
	}
	if err := c.DeleteSecret(ctx, "k"); err == nil {
		t.Fatal("del secret")
	}
	if _, err := c.InjectSecret(ctx, "x", "k", ""); err == nil {
		t.Fatal("inject")
	}
	if _, err := c.InjectSecret(ctx, "x", "k", "/run/s"); err == nil {
		t.Fatal("inject path")
	}
	if err := c.PutFile(ctx, "x", "/a", strings.NewReader("hi"), 2, client.CPOpts{}); err == nil {
		t.Fatal("putfile")
	}
	if err := c.GetFile(ctx, "x", "/a", io.Discard); err == nil {
		t.Fatal("getfile")
	}
	if err := c.PutTar(ctx, "x", "/a", strings.NewReader("t")); err == nil {
		t.Fatal("puttar")
	}
	if err := c.GetTar(ctx, "x", "/a", io.Discard); err == nil {
		t.Fatal("gettar")
	}
	if _, err := c.ReadDir(ctx, "x", "/"); err == nil {
		t.Fatal("readdir")
	}
	if _, err := c.Stat(ctx, "x", "/a"); err == nil {
		t.Fatal("stat")
	}
	if err := c.Mkdir(ctx, "x", "/a", true, "0755"); err == nil {
		t.Fatal("mkdir")
	}
	if err := c.Remove(ctx, "x", "/a", true); err == nil {
		t.Fatal("remove")
	}
}

func TestClientBadJSONBodies(t *testing.T) {
	t.Parallel()
	srv := errServer(t, 200, `not-json{`)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.Info(ctx); err == nil {
		t.Fatal("info decode")
	}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("list decode")
	}
	if _, err := c.Get(ctx, "x"); err == nil {
		t.Fatal("get decode")
	}
	if _, err := c.Start(ctx, "x"); err == nil {
		t.Fatal("start decode")
	}
	if _, err := c.Restore(ctx, "x"); err == nil {
		t.Fatal("restore decode")
	}
	if _, err := c.AddForward(ctx, "x", 1, 2); err == nil {
		t.Fatal("fwd decode")
	}
	if _, err := c.Exec(ctx, "x", "echo"); err == nil {
		t.Fatal("exec decode")
	}
	if _, err := c.AgentHealth(ctx, "x"); err == nil {
		t.Fatal("agent decode")
	}
	if _, err := c.Stats(ctx, "x"); err == nil {
		t.Fatal("stats decode")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("secrets decode")
	}
	if _, err := c.SetSecret(ctx, client.SecretPut{Name: "a", DataBase64: "Yg=="}); err == nil {
		t.Fatal("setsecret decode")
	}
	if _, err := c.ReadDir(ctx, "x", "/"); err == nil {
		t.Fatal("readdir decode")
	}
	if _, err := c.Stat(ctx, "x", "/a"); err == nil {
		t.Fatal("stat decode")
	}
}

func TestClientClosedServer(t *testing.T) {
	t.Parallel()
	srv := errServer(t, 200, `{}`)
	u := srv.URL
	srv.Close()
	c, err := client.DialHTTP(u, "tok")
	if err != nil {
		t.Fatal(err)
	}
	c.SetToken("tok2")
	if c.Token() != "tok2" {
		t.Fatal(c.Token())
	}
	if c.Base() != u {
		t.Fatal(c.Base())
	}
	ctx := context.Background()
	if err := c.Health(ctx); err == nil {
		t.Fatal("expected dial error")
	}
	if _, err := c.CreateStream(ctx, client.CreateRequest{Name: "n"}, nil); err == nil {
		t.Fatal("createstream")
	}
	_, _ = c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "true"}, nil)
}

func TestCreateStreamPartialAndInvalid(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") != "1" {
			http.Error(w, "no", 400)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		// Ready with name only (no nested instance object).
		_, _ = io.WriteString(w, `{"phase":"qemu","message":"boot"}`+"\n")
		_, _ = io.WriteString(w, `{"phase":"ready","name":"only-name"}`+"\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	inst, err := c.CreateStream(context.Background(), client.CreateRequest{Name: "only-name"}, func(ev client.CreateEvent) {
		phases = append(phases, ev.Phase)
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst == nil || inst.Name != "only-name" {
		t.Fatalf("%+v", inst)
	}
	_ = phases
}

func TestExecStreamStdoutStderrExit(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"type":"started","pid":9}`+"\n")
		_, _ = io.WriteString(w, `{"type":"stdout","data":"out\n"}`+"\n")
		_, _ = io.WriteString(w, `{"type":"stderr","data":"err\n"}`+"\n")
		_, _ = io.WriteString(w, `{"type":"exit","exit_code":3}`+"\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var frames int
	code, err := c.ExecStream(context.Background(), "vm", client.ExecOpts{
		Cmd: "sh", Args: []string{"-c", "x"},
	}, func(f client.ExecFrame) error {
		frames++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 || frames < 3 {
		t.Fatalf("code=%d frames=%d", code, frames)
	}
}

func TestPutGetFileSuccessPaths(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.URL.Query().Get("mode") == "tar" {
			_, _ = w.Write([]byte("tardata"))
			return
		}
		_, _ = w.Write([]byte("payload"))
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uid, gid := uint32(1), uint32(2)
	if err := c.PutFile(ctx, "vm", "/tmp/a", strings.NewReader("hi"), 2, client.CPOpts{
		Mode: "0644", UID: &uid, GID: &gid,
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, "vm", "/tmp/a", &buf); err != nil || buf.String() != "payload" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := c.PutTar(ctx, "vm", "/tmp", strings.NewReader("t")); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := c.GetTar(ctx, "vm", "/tmp", &buf); err != nil || buf.String() != "tardata" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := c.Mkdir(ctx, "vm", "/tmp/d", true, "0755"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, "vm", "/tmp/d", true); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Additional error-path / decode coverage
// ---------------------------------------------------------------------------

func TestClientCreateDecodeAndStreamInvalidJSON(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(200)
			_, _ = io.WriteString(w, "{\"phase\":\"qemu\"}\n")
			_, _ = io.WriteString(w, "{not-valid-json\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "not-json{")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.Create(ctx, client.CreateRequest{Name: "x"}); err == nil {
		t.Fatal("expected Create decode error")
	}
	if _, err := c.CreateStream(ctx, client.CreateRequest{Name: "x"}, nil); err == nil {
		t.Fatal("expected CreateStream event decode error")
	}
}

func TestClientInjectSecretDecodeError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/secrets/{s}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `not-json`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(context.Background(), "vm", "k", "/p"); err == nil {
		t.Fatal("expected inject decode error")
	}
}

func TestClientExecStreamHTTPError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 503, map[string]string{"error": "agent unavailable"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ExecStream(context.Background(), "vm", client.ExecOpts{Cmd: "true"}, func(client.ExecFrame) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "agent unavailable") {
		t.Fatalf("want agent unavailable, got %v", err)
	}
}

func TestClientNetworkErrorsAllMethods(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	u := srv.URL
	srv.Close()

	c, err := client.DialHTTP(u, "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Exercise do()/transport failure paths on every public method.
	_ = c.Health(ctx)
	_, _ = c.Info(ctx)
	_, _ = c.List(ctx)
	_, _ = c.Create(ctx, client.CreateRequest{Name: "n", Wait: "ssh", Timeout: "1s"})
	_, _ = c.CreateStream(ctx, client.CreateRequest{Name: "n"}, func(client.CreateEvent) {})
	_, _ = c.Get(ctx, "n")
	_ = c.Delete(ctx, "n")
	_, _ = c.Start(ctx, "n")
	_ = c.Stop(ctx, "n")
	_ = c.Shutdown(ctx, "n")
	_ = c.Pause(ctx, "n")
	_ = c.Resume(ctx, "n")
	_ = c.Suspend(ctx, "n")
	_, _ = c.Restore(ctx, "n")
	_, _ = c.AddForward(ctx, "n", 0, 80)
	_ = c.RemoveForward(ctx, "n", 8080)
	_, _ = c.Exec(ctx, "n", "true", "a")
	_, _ = c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "true", Cwd: "/"}, func(client.ExecFrame) error { return nil })
	_, _ = c.AgentHealth(ctx, "n")
	_, _ = c.Stats(ctx, "n")
	_, _ = c.ListSecrets(ctx)
	_, _ = c.SetSecret(ctx, client.SecretPut{Name: "k", DataBase64: "YQ=="})
	_ = c.DeleteSecret(ctx, "k")
	_, _ = c.InjectSecret(ctx, "n", "k", "")
	_, _ = c.InjectSecret(ctx, "n", "k", "/run/s")
	_ = c.PutFile(ctx, "n", "/a", strings.NewReader("x"), 1, client.CPOpts{})
	_ = c.GetFile(ctx, "n", "/a", io.Discard)
	_ = c.PutTar(ctx, "n", "/a", strings.NewReader("t"))
	_ = c.GetTar(ctx, "n", "/a", io.Discard)
	_, _ = c.ReadDir(ctx, "n", "/")
	_, _ = c.Stat(ctx, "n", "/a")
	_ = c.Mkdir(ctx, "n", "/a", true, "0755")
	_ = c.Remove(ctx, "n", "/a", false)
	_ = c.Remove(ctx, "n", "/a", true)
}

func TestClientCreateWithWaitUserdataStream(t *testing.T) {
	t.Parallel()
	ts := mockDaemon(t, "")
	t.Cleanup(ts.Close)
	c, err := client.DialHTTP(ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	inst, err := c.CreateStream(context.Background(), client.CreateRequest{
		Name: "ud",
		Wait: client.WaitUserdata,
	}, func(ev client.CreateEvent) {
		phases = append(phases, ev.Phase)
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "ud" {
		t.Fatalf("%+v", inst)
	}
	joined := strings.Join(phases, ",")
	if !strings.Contains(joined, "wait_userdata") && !strings.Contains(joined, client.PhaseReady) {
		t.Fatalf("phases %v", phases)
	}
}

// TestClientInvalidBaseURL exercises NewRequest/url.Parse failure branches.
// DialHTTP only rejects an empty base, so a syntactically invalid base is accepted
// and then fails on the first request builder call.
func TestClientInvalidBaseURL(t *testing.T) {
	t.Parallel()
	c, err := client.DialHTTP("%", "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Health(ctx); err == nil {
		t.Fatal("Health")
	}
	if _, err := c.Info(ctx); err == nil {
		t.Fatal("Info")
	}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("List")
	}
	if _, err := c.Create(ctx, client.CreateRequest{Name: "n", Wait: "ssh", Timeout: "1s"}); err == nil {
		t.Fatal("Create")
	}
	if _, err := c.CreateStream(ctx, client.CreateRequest{Name: "n"}, nil); err == nil {
		t.Fatal("CreateStream")
	}
	if _, err := c.Get(ctx, "n"); err == nil {
		t.Fatal("Get")
	}
	if err := c.Delete(ctx, "n"); err == nil {
		t.Fatal("Delete")
	}
	if _, err := c.Start(ctx, "n"); err == nil {
		t.Fatal("Start")
	}
	if err := c.Stop(ctx, "n"); err == nil {
		t.Fatal("Stop")
	}
	if err := c.Shutdown(ctx, "n"); err == nil {
		t.Fatal("Shutdown")
	}
	if err := c.Pause(ctx, "n"); err == nil {
		t.Fatal("Pause")
	}
	if err := c.Resume(ctx, "n"); err == nil {
		t.Fatal("Resume")
	}
	if err := c.Suspend(ctx, "n"); err == nil {
		t.Fatal("Suspend")
	}
	if _, err := c.Restore(ctx, "n"); err == nil {
		t.Fatal("Restore")
	}
	if _, err := c.AddForward(ctx, "n", 0, 80); err == nil {
		t.Fatal("AddForward")
	}
	if err := c.RemoveForward(ctx, "n", 9); err == nil {
		t.Fatal("RemoveForward")
	}
	if _, err := c.Exec(ctx, "n", "true", "a"); err == nil {
		t.Fatal("Exec")
	}
	if _, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "true", Cwd: "/tmp"}, func(client.ExecFrame) error { return nil }); err == nil {
		t.Fatal("ExecStream")
	}
	if _, err := c.AgentHealth(ctx, "n"); err == nil {
		t.Fatal("AgentHealth")
	}
	if _, err := c.Stats(ctx, "n"); err == nil {
		t.Fatal("Stats")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("ListSecrets")
	}
	if _, err := c.SetSecret(ctx, client.SecretPut{Name: "k", DataBase64: "YQ=="}); err == nil {
		t.Fatal("SetSecret")
	}
	if err := c.DeleteSecret(ctx, "k"); err == nil {
		t.Fatal("DeleteSecret")
	}
	if _, err := c.InjectSecret(ctx, "n", "k", ""); err == nil {
		t.Fatal("InjectSecret")
	}
	if _, err := c.InjectSecret(ctx, "n", "k", "/p"); err == nil {
		t.Fatal("InjectSecret path")
	}
	if err := c.PutFile(ctx, "n", "/a", strings.NewReader("x"), 1, client.CPOpts{}); err == nil {
		t.Fatal("PutFile")
	}
	if err := c.GetFile(ctx, "n", "/a", io.Discard); err == nil {
		t.Fatal("GetFile")
	}
	if err := c.PutTar(ctx, "n", "/a", strings.NewReader("t")); err == nil {
		t.Fatal("PutTar")
	}
	if err := c.GetTar(ctx, "n", "/a", io.Discard); err == nil {
		t.Fatal("GetTar")
	}
	if _, err := c.ReadDir(ctx, "n", "/"); err == nil {
		t.Fatal("ReadDir")
	}
	if _, err := c.Stat(ctx, "n", "/a"); err == nil {
		t.Fatal("Stat")
	}
	if err := c.Mkdir(ctx, "n", "/a", true, "0755"); err == nil {
		t.Fatal("Mkdir")
	}
	if err := c.Remove(ctx, "n", "/a", true); err == nil {
		t.Fatal("Remove")
	}
}

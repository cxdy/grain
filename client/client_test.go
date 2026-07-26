package client_test

import (
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
		var req client.CreateRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		name := req.Name
		if name == "" {
			name = "auto"
		}
		writeJSON(w, http.StatusCreated, &client.Instance{
			Name:   name,
			Status: client.StatusRunning,
			CPUs:   2,
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
	if err := c.Suspend(ctx, "box"); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if _, err := c.Restore(ctx, "box"); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := c.Delete(ctx, "box"); err != nil {
		t.Fatalf("Delete: %v", err)
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
	srv := &http.Server{Handler: mux}
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

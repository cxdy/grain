package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/vm"
)

// mockAPIForCP implements the daemon cp/stat/tar surface used by daemonPut/Get.
func mockAPIForCP(t *testing.T) *api.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		if mode == "tar" {
			var buf bytes.Buffer
			_ = writeLocalTar(&buf, t.TempDir())
			_, _ = w.Write(buf.Bytes())
			return
		}
		_, _ = w.Write([]byte("file-bytes"))
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		typ := "file"
		name := filepath.Base(p)
		if strings.Contains(p, "dir") {
			typ = "directory"
		}
		_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: name, Type: typ, Size: 10, Mode: "0644"})
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "vm", Status: vm.StatusRunning, AgentPort: 1})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&agent.Health{Hostname: "g", AgentVersion: "1"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &api.Client{Base: srv.URL, HTTP: srv.Client()}
}

func TestCPViaAgentPolicyErrors(t *testing.T) {
	t.Parallel()
	c := &api.Client{Base: "http://127.0.0.1:1"}
	// host to host
	if err := cpViaAgent(c, parseCPSpec("/a"), parseCPSpec("/b"), false, false); err != errAgentSkip {
		t.Fatalf("host-host: %v", err)
	}
	// guest to guest different
	if err := cpViaAgent(c, parseCPSpec("a:/x"), parseCPSpec("b:/y"), false, false); err == nil {
		t.Fatal("guest-guest")
	}
	// same guest
	if err := cpViaAgent(c, parseCPSpec("a:/x"), parseCPSpec("a:/y"), false, false); err == nil {
		t.Fatal("same guest")
	}
}

func TestDaemonPutFileAndDir(t *testing.T) {
	t.Parallel()
	c := mockAPIForCP(t)
	ctx := context.Background()
	dir := t.TempDir()
	f := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := daemonPut(ctx, c, "vm", f, "/tmp/hello.txt"); err != nil {
		t.Fatal(err)
	}
	// trailing slash guest → join base name
	if err := daemonPut(ctx, c, "vm", f, "/tmp/"); err != nil {
		t.Fatal(err)
	}
	// directory
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := daemonPut(ctx, c, "vm", sub, "/tmp/sub"); err != nil {
		t.Fatal(err)
	}
	// missing src
	if err := daemonPut(ctx, c, "vm", filepath.Join(dir, "nope"), "/x"); err == nil {
		t.Fatal("missing")
	}
}

func TestDaemonGetFileAndDir(t *testing.T) {
	t.Parallel()
	c := mockAPIForCP(t)
	ctx := context.Background()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	if err := daemonGet(ctx, c, "vm", "/tmp/out.txt", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "file-bytes" {
		t.Fatalf("%q %v", b, err)
	}
	// host path is directory
	destDir := filepath.Join(dir, "d")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := daemonGet(ctx, c, "vm", "/tmp/x", destDir); err != nil {
		t.Fatal(err)
	}
	// trailing slash
	if err := daemonGet(ctx, c, "vm", "/tmp/x", destDir+string(os.PathSeparator)); err != nil {
		t.Fatal(err)
	}
	// directory type guest path
	destDir2 := filepath.Join(dir, "fromdir")
	if err := daemonGet(ctx, c, "vm", "/tmp/dir", destDir2); err != nil {
		t.Fatal(err)
	}
}

func TestAgentPutGetWithMockAgent(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "tar" {
			var buf bytes.Buffer
			tmp := t.TempDir()
			_ = os.WriteFile(filepath.Join(tmp, "z.txt"), []byte("z"), 0o644)
			_ = writeLocalTar(&buf, tmp)
			_, _ = w.Write(buf.Bytes())
			return
		}
		_, _ = w.Write([]byte("from-agent"))
	})
	mux.HandleFunc("GET /fs/stat", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		typ := "file"
		if strings.Contains(p, "dir") {
			typ = "directory"
		}
		_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: filepath.Base(p), Type: typ, Size: 3, Mode: "0644"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ac := &agent.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(src, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agentPut(ctx, ac, src, "/tmp/s.txt"); err != nil {
		t.Fatal(err)
	}
	if err := agentPut(ctx, ac, src, "/tmp/"); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "folder")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "i.txt"), []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agentPut(ctx, ac, sub, "/tmp/folder"); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "got.txt")
	if err := agentGet(ctx, ac, "/tmp/got.txt", out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if string(b) != "from-agent" {
		t.Fatalf("%q", b)
	}
	// into existing dir
	dd := filepath.Join(dir, "into")
	_ = os.MkdirAll(dd, 0o755)
	if err := agentGet(ctx, ac, "/tmp/got.txt", dd); err != nil {
		t.Fatal(err)
	}
	// directory pull
	dest := filepath.Join(dir, "pulldir")
	if err := agentGet(ctx, ac, "/tmp/dir", dest); err != nil {
		t.Fatal(err)
	}
	// missing src for put
	if err := agentPut(ctx, ac, filepath.Join(dir, "missing"), "/x"); err == nil {
		t.Fatal("missing")
	}
}

func TestCPViaAgentDaemonMode(t *testing.T) {
	t.Parallel()
	c := mockAPIForCP(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cpViaAgent(c, parseCPSpec(src), parseCPSpec("vm:/tmp/x.txt"), false, true); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "y.txt")
	if err := cpViaAgent(c, parseCPSpec("vm:/tmp/y.txt"), parseCPSpec(out), false, true); err != nil {
		t.Fatal(err)
	}
}

func TestCmdCpForceFlags(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(cfg, []byte("data_dir: /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdCp(&cfg)
	cmd.SetArgs([]string{"--ssh", "--agent", "a", "b"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("%v", err)
	}
}

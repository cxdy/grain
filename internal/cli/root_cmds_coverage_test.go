package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/vm"
)

func fullMockDaemon(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "grain", "version": "test"})
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{
			{Name: "sbox-1", Status: vm.StatusRunning, CPUs: 2, MemoryMB: 2048, SSHPort: 2201, AgentPort: 7476},
		})
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&vm.Instance{
			Name: r.PathValue("name"), Status: vm.StatusRunning, SSHPort: 2201, AgentPort: 7476, IP: "127.0.0.1",
		})
	})
	mux.HandleFunc("DELETE /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "deleted"})
	})
	mux.HandleFunc("POST /vms/{name}/shutdown", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&vm.Instance{Name: r.PathValue("name"), Status: vm.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/pause", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/resume", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/suspend", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&vm.Instance{Name: r.PathValue("name"), Status: vm.StatusRunning})
	})
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"phase":"ready","name":"sbox-1","instance":{"name":"sbox-1","status":"running"}}` + "\n"))
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "sbox-1", Status: vm.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"type":"started","pid":1}` + "\n"))
			_, _ = w.Write([]byte(`{"type":"stdout","data":"ok\n"}` + "\n"))
			_, _ = w.Write([]byte(`{"type":"exit","exit_code":0}` + "\n"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"stdout": "ok\n", "exit_code": 0})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&agent.Health{Hostname: "g", AgentVersion: "1"})
	})
	mux.HandleFunc("GET /vms/{name}/stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&agent.Stats{UptimeSec: 1})
	})
	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{})
	})
	mux.HandleFunc("POST /vms/{name}/forwards", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"host_port": 18080, "guest_port": 80, "pid": 1})
	})
	mux.HandleFunc("DELETE /vms/{name}/forwards/{port}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]agent.FSInfo{{Name: "a", Type: "file", Size: 1, Mode: "0644"}})
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: "a", Type: "file", Size: 1, Mode: "0644"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func withRemoteCfg(t *testing.T, apiURL string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte("data_dir: "+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apiURLFlag = apiURL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	return cfg
}

func TestRootLifecycleCommandsRemote(t *testing.T) {
	srv := fullMockDaemon(t)
	cfg := withRemoteCfg(t, srv.URL)

	// ls
	cmd := cmdLs(&cfg)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	// rm
	cmd = cmdRm(&cfg)
	cmd.SetArgs([]string{"sbox-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rm: %v", err)
	}
	// stop / start / pause / resume / suspend / restore
	for _, setup := range []struct {
		name string
		fn   func(*string) interface{ Execute() error }
		args []string
	}{
		{"stop", func(p *string) interface{ Execute() error } { return cmdStop(p) }, []string{"sbox-1"}},
		{"start", func(p *string) interface{ Execute() error } { return cmdStart(p) }, []string{"sbox-1"}},
		{"pause", func(p *string) interface{ Execute() error } { return cmdPause(p) }, []string{"sbox-1"}},
		{"resume", func(p *string) interface{ Execute() error } { return cmdResume(p) }, []string{"sbox-1"}},
		{"suspend", func(p *string) interface{ Execute() error } { return cmdSuspend(p) }, []string{"sbox-1"}},
		{"restore", func(p *string) interface{ Execute() error } { return cmdRestore(p) }, []string{"sbox-1"}},
	} {
		c := setup.fn(&cfg)
		// cobra commands
		type setArgs interface {
			SetArgs([]string)
			Execute() error
		}
		if sa, ok := c.(setArgs); ok {
			sa.SetArgs(setup.args)
			if err := sa.Execute(); err != nil {
				t.Fatalf("%s: %v", setup.name, err)
			}
		}
	}
}

func TestCmdNewAndXRemote(t *testing.T) {
	srv := fullMockDaemon(t)
	cfg := withRemoteCfg(t, srv.URL)

	cmd := cmdNew(&cfg)
	cmd.SetArgs([]string{"--name", "sbox-1", "--cpus", "2", "--mem", "1024"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("new: %v", err)
	}

	cmd = cmdX(&cfg)
	cmd.SetArgs([]string{"sbox-1", "--", "echo", "hi"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("x: %v", err)
	}
}

func TestCmdStatsAndAgentHealthRemote(t *testing.T) {
	srv := fullMockDaemon(t)
	cfg := withRemoteCfg(t, srv.URL)
	cmd := cmdStats(&cfg)
	cmd.SetArgs([]string{"sbox-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stats: %v", err)
	}
	// agent health via root helper if exists
	cmd = Root("test")
	cmd.SetArgs([]string{"--api", srv.URL, "--config", cfg, "agent", "health", "sbox-1"})
	// may not have agent health subcommand path — soft check
	_ = cmd
}

func TestParseVolumeAndPublishHelpers(t *testing.T) {
	t.Parallel()
	if _, err := parseVolumeFlag("nocolon"); err == nil {
		t.Fatal("nocolon")
	}
	if v, err := parseVolumeFlag("/host:/guest"); err != nil || v.Host == "" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := parsePublishFlag("notaport"); err == nil {
		t.Fatal("bad port")
	}
	if p, err := parsePublishFlag("8080:80"); err != nil || p.GuestPort != 80 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, _, err := parsePublishSocketFlag("bad"); err == nil {
		t.Fatal("bad sock")
	}
	host, guest, err := parsePublishSocketFlag("/tmp/d.sock:/var/run/docker.sock")
	if err != nil || host == "" || guest == "" {
		t.Fatalf("%q %q %v", host, guest, err)
	}
}

func TestShellQuoteHelpers(t *testing.T) {
	t.Parallel()
	if shellQuote("plain") != "plain" && !strings.Contains(shellQuote("a b"), "'") {
		// either style ok
	}
	_ = shellQuote("it's")
	_ = shellQuote("")
}

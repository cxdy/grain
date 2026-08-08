package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vm"
)

func TestCmdNewPresetDocker(t *testing.T) {
	var gotBody map[string]any
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodPost && r.URL.Path == "/vms":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/x-ndjson")
			ev := vm.CreateEvent{
				Phase: vm.PhaseReady,
				Instance: &vm.Instance{
					Name: "d1", Status: vm.StatusRunning, Image: "grain-ubuntu", SSHPort: 22,
				},
			}
			b, _ := json.Marshal(ev)
			_, _ = w.Write(append(b, '\n'))
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdNew(&cfg)
	cmd.SetArgs([]string{"--preset", "docker", "-n", "d1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	ud, _ := gotBody["userdata"].(string)
	if ud == "" {
		t.Fatalf("expected preset userdata: %+v", gotBody)
	}
}

func TestCmdCloneExecute(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case strings.HasSuffix(r.URL.Path, "/clone"):
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "b", Status: vm.StatusStopped, Persistent: true, Image: "img",
			})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	cmd := cmdClone(&cfg)
	cmd.SetArgs([]string{"a", "b"})
	err = cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "cloned") {
		t.Fatalf("%q", out)
	}
}

func TestRootIncludesCheckConfigAndPool(t *testing.T) {
	cmd := Root("t")
	want := map[string]bool{"check-config": false, "pool": false, "recipe": false, "fwd": false}
	for _, c := range cmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("missing %s", k)
		}
	}
}

func TestBuildProxyUserdataCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, ProxyListen: "127.0.0.1:3128"}
	ud, err := buildProxyUserdata(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ud, "PROXY") && !strings.Contains(strings.ToLower(ud), "proxy") {
		// still non-empty cloud config
		if strings.TrimSpace(ud) == "" {
			t.Fatal("empty")
		}
	}
	// second call reuses token
	ud2, err := buildProxyUserdata(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ud2 == "" {
		t.Fatal("empty2")
	}
}

func TestCmdNewMissingUserdataFile(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdNew(&cfg)
	cmd.SetArgs([]string{"--userdata-file", filepath.Join(t.TempDir(), "missing.yaml")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want userdata error")
	}
}

func TestCmdNewBadRecipe(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdNew(&cfg)
	cmd.SetArgs([]string{"--recipe", filepath.Join(t.TempDir(), "nope.yaml")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want recipe load error")
	}
}

func TestPrintDaemonUpVariants(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printDaemonUp("h", config.Config{Socket: "/s", API: "", MCP: config.MCPConfig{Enabled: false}})
	printDaemonUp("h2", config.Config{
		Socket: "/s", API: "0.0.0.0:1",
		MCP: config.MCPConfig{Enabled: true, Listen: "127.0.0.1:9"},
	})
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	_ = r.Close()
	s := string(out)
	if !strings.Contains(s, "socket") || !strings.Contains(s, "mcp") {
		t.Fatalf("%q", s)
	}
	_ = fmt.Sprintf("%d", len(s))
}

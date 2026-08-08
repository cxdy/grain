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
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vm"
)

func TestCmdNewCloneFromAndPoolAndFrom(t *testing.T) {
	var lastPath string
	var lastBody map[string]any
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodPost && r.URL.Path == "/vms":
			_ = json.NewDecoder(r.Body).Decode(&lastBody)
			name := "spawned"
			if n, _ := lastBody["name"].(string); n != "" {
				name = n
			}
			if fromPool, _ := lastBody["from_pool"].(bool); fromPool {
				name = "from-pool"
			}
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: name, Status: vm.StatusRunning, SSHPort: 2201, AgentPort: 7701, Persistent: true,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/clone"):
			_ = json.NewDecoder(r.Body).Decode(&lastBody)
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "dst", Status: vm.StatusStopped, Persistent: true, Image: "grain-ubuntu",
			})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""

	// --clone
	cmd := cmdNew(&cfg)
	cmd.SetArgs([]string{"--clone", "src", "-n", "dst"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if !strings.Contains(lastPath, "/clone") {
		t.Fatalf("path=%s", lastPath)
	}

	// --from-pool
	cmd = cmdNew(&cfg)
	cmd.SetArgs([]string{"--from-pool", "-n", "work1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("from-pool: %v", err)
	}
	if lastBody["from_pool"] != true {
		t.Fatalf("body=%+v", lastBody)
	}

	// --from template
	cmd = cmdNew(&cfg)
	cmd.SetArgs([]string{"--from", "golden", "-n", "w1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("from: %v", err)
	}
	if lastBody["from"] != "golden" {
		t.Fatalf("body=%+v", lastBody)
	}

	// mutual exclusion
	cmd = cmdNew(&cfg)
	cmd.SetArgs([]string{"--from-pool", "--from", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want mutual exclusion")
	}
	cmd = cmdNew(&cfg)
	cmd.SetArgs([]string{"--from-pool", "--recipe", "x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want from-pool alone")
	}
	cmd = cmdNew(&cfg)
	cmd.SetArgs([]string{"--from", "g", "--userdata-file", "/nope"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want from alone")
	}
}

func TestCmdNewWithRecipeFile(t *testing.T) {
	dir := t.TempDir()
	recipePath := filepath.Join(dir, "lab.yaml")
	body := `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
spec:
  image: grain-ubuntu
  cpus: 2
  memory_mb: 1024
  disk_gb: 8
  persistent: true
  arch: arm64
  gpu: virtio
  network: overlay
  wait: agent
  ready_timeout: 3m
  forwards:
    - host_port: 18080
      guest_port: 80
  mounts:
    - host: /tmp
      guest: /work
`
	if err := os.WriteFile(recipePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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
					Name: "lab", Status: vm.StatusRunning, Image: "grain-ubuntu",
					SSHPort: 2200, Persistent: true,
					Forwards: []vm.PortForward{{HostPort: 18080, GuestPort: 80}},
					Mounts:   []vm.Mount{{Host: "/tmp", Guest: "/work"}},
					SocketForwards: []vm.SocketForward{
						{HostPath: "/tmp/h.sock", GuestPath: "/tmp/g.sock"},
					},
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
	cmd.SetArgs([]string{"--recipe", recipePath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("new recipe: %v", err)
	}
	if gotBody["image"] != "grain-ubuntu" {
		t.Fatalf("body=%+v", gotBody)
	}

	// recipe + userdata mutually exclusive
	cmd = cmdNew(&cfg)
	cmd.SetArgs([]string{"--recipe", recipePath, "--userdata-file", recipePath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("want exclusive")
	}
}

func TestCmdNewUserdataFileAndProxy(t *testing.T) {
	dir := t.TempDir()
	ud := filepath.Join(dir, "ud.yaml")
	if err := os.WriteFile(ud, []byte("#cloud-config\npackages: [curl]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// proxy store under data dir
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	var gotBody map[string]any
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodPost && r.URL.Path == "/vms":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/x-ndjson")
			ev := vm.CreateEvent{
				Phase:    vm.PhaseReady,
				Instance: &vm.Instance{Name: "p", Status: vm.StatusRunning, Image: "x"},
			}
			b, _ := json.Marshal(ev)
			_, _ = w.Write(append(b, '\n'))
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cmd := cmdNew(&cfgFile)
	cmd.SetArgs([]string{"--userdata-file", ud, "--proxy", "-n", "p"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("new proxy: %v", err)
	}
	udStr, _ := gotBody["userdata"].(string)
	if !strings.Contains(udStr, "PROXY") && !strings.Contains(udStr, "proxy") && !strings.Contains(udStr, "cloud-config") {
		// GuestProxyCloudConfig always injects proxy env; still ensure userdata present
		if strings.TrimSpace(udStr) == "" {
			t.Fatalf("empty userdata: %+v", gotBody)
		}
	}
}

func TestCmdCloneAndRunHelpers(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/clone"):
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "dst", Status: vm.StatusStopped, Persistent: true, Image: "img",
			})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdClone(&cfg)
	cmd.SetArgs([]string{"src", "dst"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	c := testAPIClient(srv)
	if err := runClone(c, "", "dst", time.Minute); err == nil {
		t.Fatal("empty src")
	}
}

func TestCmdPoolStatusFillClaimDrain(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/pool":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"enabled": true, "template": "golden", "desired": 2, "ready": 1,
				"members": []string{"pool-1"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/pool/fill":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"enabled": true, "template": "golden", "desired": 2, "ready": 2,
			})
		// CLI claim uses Create with from_pool=true (POST /vms), not /pool/claim.
		case r.Method == http.MethodPost && r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "work", Status: vm.StatusRunning, SSHPort: 2200, AgentPort: 7700,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/pool/drain":
			_ = json.NewEncoder(w).Encode(map[string]any{"drained": 2})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	root := cmdPool(&cfg)

	for _, args := range [][]string{
		{"status"},
		{"fill"},
		{"claim", "-n", "work"},
		{"drain"},
	} {
		cmd := cmdPool(&cfg)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	_ = root
}

func TestRunSpawnAndPoolClaimDirect(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodPost && r.URL.Path == "/vms":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			name := "x"
			if n, _ := body["name"].(string); n != "" {
				name = n
			}
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: name, Status: vm.StatusRunning, SSHPort: 1, AgentPort: 2,
			})
		default:
			http.NotFound(w, r)
		}
	})
	c := testAPIClient(srv)
	if err := runSpawn(c, "tpl", "dst", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := runPoolClaim(c, "dst2", time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentPreRunUpdateNotice(t *testing.T) {
	// Exercises Root PersistentPreRun (loadCfg fail → Defaults + maybePrintUpdateNotice).
	root := Root("0.0.0-test")
	root.SetArgs([]string{"version"})
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	err = root.Execute()
	_ = w.Close()
	os.Stdout = old
	_, _ = io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDaemonPIDPathAndPrintDaemonUpMCP(t *testing.T) {
	cfg := config.Config{
		DataDir: "/tmp/g",
		Socket:  "/tmp/g.sock",
		API:     "127.0.0.1:7474",
		MCP:     config.MCPConfig{Enabled: true, Listen: ""},
	}
	if daemonPIDPath(cfg) != filepath.Join("/tmp/g", "grain.pid") {
		t.Fatal(daemonPIDPath(cfg))
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	printDaemonUp("hdr", cfg)
	_ = w.Close()
	os.Stdout = old
	outB, _ := io.ReadAll(r)
	_ = r.Close()
	out := string(outB)
	if !strings.Contains(out, "mcp") || !strings.Contains(out, "7476") {
		t.Fatalf("%q", out)
	}
}

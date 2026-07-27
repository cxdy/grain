package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/proxy"
	"github.com/cxdy/grain/internal/vm"
)

func TestCmdDoctorRuns(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	body := "data_dir: " + dir + "\nhypervisor: mock\nlog_level: error\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// runDoctor exercises dep checks; overall status may still be non-nil.
	_ = cmdDoctor(&cfgPath).Execute()
}

func TestCmdImageLsViaCobra(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\nhypervisor: mock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	img := cmdImage(&cfgPath)
	img.SetArgs([]string{"ls"})
	if err := img.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdImagePullDefaultViaCobra(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	id := "ubuntu-cloud"
	d := filepath.Join(dir, "images", id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 2*1024*1024)
	for i := range payload {
		payload[i] = byte(i)
	}
	if err := os.WriteFile(filepath.Join(d, "disk.qcow2"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "c.yaml")
	yml := "data_dir: " + dir + "\nhypervisor: mock\nimage: " + id + "\n"
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	img := cmdImage(&cfgPath)
	img.SetArgs([]string{"pull"})
	if err := img.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdProxyUpAlreadyRunningPID(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dataDir := t.TempDir()
	cfgPath := writeProxyConfig(t, dataDir)
	pidPath := proxy.PIDPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	up := cmdProxyUp(&cfgPath)
	up.SetArgs([]string{})
	if err := up.Execute(); err != nil {
		t.Fatalf("already up: %v", err)
	}
}

func TestCmdCpViaSCPForceSSH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms/box" {
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "box", Status: vm.StatusRunning, IP: "127.0.0.1", SSHPort: 1,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	src := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(src, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdCp(&cfg)
	cmd.SetArgs([]string{"--ssh", src, "box:/tmp/f.txt"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected scp failure against dead port")
	}
}

func TestShellViaAgentNoEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/vms/") {
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "n", Status: vm.StatusRunning})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if err := shellViaAgent(c, "n", false, false); err == nil {
		t.Fatal("expected agent skip/unavailable")
	}
	if err := shellViaAgent(c, "n", true, false); err == nil {
		t.Fatal("expected force error")
	}
}

func TestShellViaAgentViaDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no ws", 500)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client(), Token: "tok"}
	if err := shellViaAgent(c, "vm1", false, true); err == nil {
		t.Fatal("expected daemon shell error")
	}
}

func TestShellViaAgentLiveDial(t *testing.T) {
	asrv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- asrv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = asrv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := asrv.AddrString()
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no agent port")
	}
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/vms/") {
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "live", Status: vm.StatusRunning, AgentPort: port, IP: "127.0.0.1",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer apiSrv.Close()
	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}
	// Shell tries websocket upgrade and fails; dial + health succeed.
	_ = shellViaAgent(c, "live", true, false)
}

func TestCmdShForceAgentUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "s", Status: vm.StatusRunning}})
		case r.URL.Path == "/vms/s":
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "s", Status: vm.StatusRunning})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd := cmdSh(&cfg)
	cmd.SetArgs([]string{"--agent", "s"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected agent unavailable")
	}
}

func TestCmdShForceSSHDeadPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms/s":
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "s", Status: vm.StatusRunning, IP: "127.0.0.1", SSHPort: 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd := cmdSh(&cfg)
	cmd.SetArgs([]string{"--ssh", "s"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected ssh failure")
	}
}

func TestCmdUpRemoteRejected(t *testing.T) {
	apiURLFlag = "http://example.com:9999"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	cfg := ""
	err := cmdUp(&cfg).Execute()
	if err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("want local-only error, got %v", err)
	}
}

func TestCmdStatsErrorPaths(t *testing.T) {
	apiURLFlag = "http://127.0.0.1:1"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd := cmdStats(&cfg)
	cmd.SetArgs([]string{"x"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected daemon down")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "only", Status: "running"}})
			return
		}
		http.Error(w, "no stats", 500)
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	cmd2 := cmdStats(&cfg)
	if err := cmd2.Execute(); err == nil {
		t.Fatal("expected stats error")
	}
}

func TestCmdFsErrorPaths(t *testing.T) {
	apiURLFlag = "http://127.0.0.1:1"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	for _, fn := range []func() error{
		func() error {
			c := cmdFsLs(&cfg)
			c.SetArgs([]string{"v", "/"})
			return c.Execute()
		},
		func() error {
			c := cmdFsStat(&cfg)
			c.SetArgs([]string{"v", "/"})
			return c.Execute()
		},
		func() error {
			c := cmdFsMkdir(&cfg)
			c.SetArgs([]string{"v", "/tmp/x"})
			return c.Execute()
		},
		func() error {
			c := cmdFsRm(&cfg)
			c.SetArgs([]string{"v", "/tmp/x"})
			return c.Execute()
		},
	} {
		if err := fn(); err == nil {
			t.Fatal("expected connection error")
		}
	}
}

func TestCmdFwdErrorPaths(t *testing.T) {
	apiURLFlag = "http://127.0.0.1:1"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	add := cmdFwdAdd(&cfg)
	add.SetArgs([]string{"v", "8080:80"})
	if err := add.Execute(); err == nil {
		t.Fatal("expected add error")
	}
	rm := cmdFwdRm(&cfg)
	rm.SetArgs([]string{"v", "8080"})
	if err := rm.Execute(); err == nil {
		t.Fatal("expected rm error")
	}
}

func TestCmdCpAgentUnavailableFallback(t *testing.T) {
	// Use --ssh so we always hit cpViaSCP (remote API mode would use daemon PutFile).
	// Clear api flag so remoteMode is false and scp runs against dead SSH port.
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only answer exact Get for SSH resolution.
		if r.Method == http.MethodGet && r.URL.Path == "/vms/box" {
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "box", Status: vm.StatusRunning, IP: "127.0.0.1", SSHPort: 1, AgentPort: 0,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	// Point at server without marking remoteMode: write a config with socket that
	// won't be used because we set api via GRAIN_API… which enables remoteMode.
	// Instead set apiURLFlag and use --ssh which still calls getVMSSH then scp.
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	cfg := ""
	src := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdCp(&cfg)
	cmd.SetArgs([]string{"--ssh", src, "box:/tmp/f"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected scp error")
	}
}

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vm"
)

func mockDaemon(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func useRemoteAPI(t *testing.T, url string) {
	t.Helper()
	apiURLFlag = url
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
}

func testAPIClient(srv *httptest.Server) *api.Client {
	return &api.Client{Base: srv.URL, HTTP: srv.Client()}
}

func TestResolveVMName(t *testing.T) {
	// explicit name
	name, err := resolveVMName(nil, []string{"explicit"}, false)
	if err != nil || name != "explicit" {
		t.Fatalf("%q %v", name, err)
	}
	// "--" is not treated as a name
	// empty list
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{})
			return
		}
		http.NotFound(w, r)
	})
	c := testAPIClient(srv)
	if _, err := resolveVMName(c, nil, false); err == nil || !strings.Contains(err.Error(), "no vms") {
		t.Fatalf("empty: %v", err)
	}

	// single VM
	srv2 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "only", Status: "running"}})
	})
	name, err = resolveVMName(testAPIClient(srv2), nil, false)
	if err != nil || name != "only" {
		t.Fatalf("single: %q %v", name, err)
	}

	// multi
	srv3 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{
			{Name: "a", Status: "running"},
			{Name: "b", Status: "running"},
		})
	})
	if _, err := resolveVMName(testAPIClient(srv3), nil, false); err == nil || !strings.Contains(err.Error(), "which vm") {
		t.Fatalf("multi: %v", err)
	}

	// createIfEmpty with stream
	srv4 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/vms" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			ev := vm.CreateEvent{
				Phase: vm.PhaseReady,
				Name:  "auto-1",
				Instance: &vm.Instance{
					Name: "auto-1", Status: vm.StatusRunning, SSHPort: 2201,
				},
			}
			b, _ := json.Marshal(ev)
			_, _ = w.Write(append(b, '\n'))
			return
		}
		http.NotFound(w, r)
	})
	name, err = resolveVMName(testAPIClient(srv4), nil, true)
	if err != nil || name != "auto-1" {
		t.Fatalf("createIfEmpty: %q %v", name, err)
	}
}

func TestGetVMSSH(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/vms/") {
			name := strings.TrimPrefix(r.URL.Path, "/vms/")
			if name == "nossh" {
				_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "nossh", SSHPort: 0})
				return
			}
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: name, SSHPort: 2222})
			return
		}
		http.NotFound(w, r)
	})
	c := testAPIClient(srv)
	host, port, err := getVMSSH(c, "sbox-1")
	if err != nil || host != "127.0.0.1" || port != 2222 {
		t.Fatalf("%s %d %v", host, port, err)
	}
	if _, _, err := getVMSSH(c, "nossh"); err == nil {
		t.Fatal("expected no ssh port error")
	}
}

func TestCmdLsRmStopPauseResume(t *testing.T) {
	var deleted, shutdown, paused, resumed, suspended, restored, started string
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{
				{Name: "sbox-1", Status: "running", CPUs: 2, MemoryMB: 1024, SSHPort: 2200, Persistent: false},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/vms/"):
			name := strings.TrimPrefix(r.URL.Path, "/vms/")
			// strip action suffix
			for _, suf := range []string{"/start", "/shutdown", "/pause", "/resume", "/suspend", "/restore"} {
				if strings.HasSuffix(r.URL.Path, suf) {
					name = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vms/"), suf)
					break
				}
			}
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: name, Status: "running", SSHPort: 2200})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/vms/"):
			deleted = strings.TrimPrefix(r.URL.Path, "/vms/")
			w.WriteHeader(204)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/shutdown"):
			shutdown = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vms/"), "/shutdown")
			w.WriteHeader(204)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pause"):
			paused = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vms/"), "/pause")
			w.WriteHeader(204)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resume"):
			resumed = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vms/"), "/resume")
			w.WriteHeader(204)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/suspend"):
			suspended = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vms/"), "/suspend")
			w.WriteHeader(204)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/restore"):
			restored = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vms/"), "/restore")
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: restored, Status: "running", SSHPort: 2200})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
			started = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/vms/"), "/start")
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: started, Status: "running", SSHPort: 2200})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""

	if err := cmdLs(&cfg).Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}

	// empty list path
	srvEmpty := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{})
			return
		}
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srvEmpty.URL)
	if err := cmdLs(&cfg).Execute(); err != nil {
		t.Fatalf("ls empty: %v", err)
	}

	useRemoteAPI(t, srv.URL)

	rm := cmdRm(&cfg)
	rm.SetArgs([]string{"sbox-1"})
	if err := rm.Execute(); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if deleted != "sbox-1" {
		t.Fatalf("deleted=%q", deleted)
	}

	stop := cmdStop(&cfg)
	stop.SetArgs([]string{"sbox-1"})
	if err := stop.Execute(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if shutdown != "sbox-1" {
		t.Fatalf("shutdown=%q", shutdown)
	}

	pause := cmdPause(&cfg)
	pause.SetArgs([]string{"sbox-1"})
	if err := pause.Execute(); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if paused != "sbox-1" {
		t.Fatalf("paused=%q", paused)
	}

	resume := cmdResume(&cfg)
	resume.SetArgs([]string{"sbox-1"})
	if err := resume.Execute(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed != "sbox-1" {
		t.Fatalf("resumed=%q", resumed)
	}

	sus := cmdSuspend(&cfg)
	sus.SetArgs([]string{"sbox-1"})
	if err := sus.Execute(); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if suspended != "sbox-1" {
		t.Fatalf("suspended=%q", suspended)
	}

	rest := cmdRestore(&cfg)
	rest.SetArgs([]string{"sbox-1"})
	if err := rest.Execute(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored != "sbox-1" {
		t.Fatalf("restored=%q", restored)
	}

	start := cmdStart(&cfg)
	start.SetArgs([]string{"sbox-1"})
	if err := start.Execute(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if started != "sbox-1" {
		t.Fatalf("started=%q", started)
	}
}

func TestCmdFwdLsAddRm(t *testing.T) {
	var addedHost, addedGuest int
	var removedPort int
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{
				Name: "sbox-1", Status: "running", SSHPort: 2200,
				Forwards:     []vm.PortForward{{HostPort: 8080, GuestPort: 80, Proto: "tcp"}},
				LiveForwards: []vm.LiveForward{{HostPort: 9090, GuestPort: 90, PID: 123}},
			}})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/vms/"):
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "sbox-1", Status: "running", SSHPort: 2200,
				Forwards: []vm.PortForward{{HostPort: 8080, GuestPort: 80}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/forwards"):
			var body struct {
				HostPort  int `json:"host_port"`
				GuestPort int `json:"guest_port"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			addedHost, addedGuest = body.HostPort, body.GuestPort
			_ = json.NewEncoder(w).Encode(&vm.LiveForward{HostPort: body.HostPort, GuestPort: body.GuestPort, PID: 99})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/forwards/"):
			parts := strings.Split(r.URL.Path, "/")
			// /vms/sbox-1/forwards/9090
			var p int
			_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &p)
			removedPort = p
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""

	if err := cmdFwdLs(&cfg).Execute(); err != nil {
		t.Fatalf("fwd ls: %v", err)
	}
	lsOne := cmdFwdLs(&cfg)
	lsOne.SetArgs([]string{"sbox-1"})
	if err := lsOne.Execute(); err != nil {
		t.Fatalf("fwd ls one: %v", err)
	}

	add := cmdFwdAdd(&cfg)
	add.SetArgs([]string{"sbox-1", "18080:80"})
	if err := add.Execute(); err != nil {
		t.Fatalf("fwd add: %v", err)
	}
	if addedHost != 18080 || addedGuest != 80 {
		t.Fatalf("added %d→%d", addedHost, addedGuest)
	}

	rm := cmdFwdRm(&cfg)
	rm.SetArgs([]string{"sbox-1", "9090"})
	if err := rm.Execute(); err != nil {
		t.Fatalf("fwd rm: %v", err)
	}
	if removedPort != 9090 {
		t.Fatalf("removed=%d", removedPort)
	}
}

func TestCmdAgentHealth(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1", Status: "running"}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/agent/health") {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "version": "test"})
			return
		}
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	root := cmdAgent(&cfg)
	root.SetArgs([]string{"health", "sbox-1"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdFsRemote(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1", Status: "running"}})
		case strings.Contains(r.URL.Path, "/fs/readdir"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "a.txt", "type": "file", "mode": "0644", "size": 3},
				{"name": "d", "type": "", "mode": "0755", "size": 0},
			})
		case strings.Contains(r.URL.Path, "/fs/stat"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "a.txt", "type": "file", "mode": "0644", "size": 3, "mtime": 1700000000,
			})
		case strings.Contains(r.URL.Path, "/fs/mkdir"):
			w.WriteHeader(204)
		case strings.Contains(r.URL.Path, "/fs/remove"):
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""

	ls := cmdFsLs(&cfg)
	ls.SetArgs([]string{"sbox-1", "/tmp"})
	if err := ls.Execute(); err != nil {
		t.Fatalf("fs ls: %v", err)
	}
	st := cmdFsStat(&cfg)
	st.SetArgs([]string{"sbox-1", "/tmp/a.txt"})
	if err := st.Execute(); err != nil {
		t.Fatalf("fs stat: %v", err)
	}
	mk := cmdFsMkdir(&cfg)
	mk.SetArgs([]string{"--parents", "sbox-1", "/tmp/n"})
	if err := mk.Execute(); err != nil {
		t.Fatalf("fs mkdir: %v", err)
	}
	rm := cmdFsRm(&cfg)
	rm.SetArgs([]string{"--recursive", "sbox-1", "/tmp/n"})
	if err := rm.Execute(); err != nil {
		t.Fatalf("fs rm: %v", err)
	}
}

func TestCmdLogsDump(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("data_dir: "+strconvQuote(dir)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// write serial log
	vmDir := filepath.Join(dir, "vms", "sbox-1")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "serial.log"), []byte("serial line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte(`{"name":"sbox-1","status":"running"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cmdLogs(&cfgPath)
	cmd.SetArgs([]string{"sbox-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logs: %v", err)
	}

	// Auto-resolve via listLocalVMNames when daemon List fails.
	// Point socket at a missing path so the default local client errors, then
	// fall back to scanning data_dir/vms (requireLocalDaemon still allows local).
	cfgPath2 := filepath.Join(dir, "config2.yaml")
	sock := filepath.Join(dir, "no-such.sock")
	if err := os.WriteFile(cfgPath2, []byte(
		"data_dir: "+strconvQuote(dir)+"\nsocket: "+strconvQuote(sock)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	cfg2, err := loadCfg(&cfgPath2)
	if err != nil {
		t.Fatal(err)
	}
	name, err := resolveLogsVMName(cfg2, nil)
	if err != nil {
		t.Fatalf("resolve local: %v", err)
	}
	if name != "sbox-1" {
		t.Fatalf("name %q", name)
	}
}

func strconvQuote(s string) string {
	return `"` + s + `"`
}

func TestResolveLogsVMName(t *testing.T) {
	cfg := configDataDir(t)
	name, err := resolveLogsVMName(cfg, []string{"given"})
	if err != nil || name != "given" {
		t.Fatalf("%q %v", name, err)
	}

	// daemon multi
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "a"}, {Name: "b"}})
	})
	useRemoteAPI(t, srv.URL)
	_, err = resolveLogsVMName(cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "which vm") {
		t.Fatalf("%v", err)
	}

	// daemon single
	srv2 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "solo"}})
	})
	useRemoteAPI(t, srv2.URL)
	name, err = resolveLogsVMName(cfg, nil)
	if err != nil || name != "solo" {
		t.Fatalf("%q %v", name, err)
	}

	// daemon empty
	srv3 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{})
	})
	useRemoteAPI(t, srv3.URL)
	if _, err := resolveLogsVMName(cfg, nil); err == nil {
		t.Fatal("expected no vms")
	}
}

func configDataDir(t *testing.T) config.Config {
	t.Helper()
	return config.Config{DataDir: t.TempDir(), Socket: filepath.Join(t.TempDir(), "x.sock")}
}

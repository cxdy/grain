package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vm"
)

func TestRemoteModeAndClientFromLocal(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := config.Config{Socket: filepath.Join(t.TempDir(), "grain.sock")}
	if remoteMode(cfg) {
		t.Fatal("expected local mode")
	}
	c, err := clientFrom(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c.Base != "http://grain" {
		t.Fatalf("base %q", c.Base)
	}
	if c.HTTP == nil {
		t.Fatal("expected unix transport client")
	}
}

func TestLoadCfgMissingOK(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope.yaml")
	cfg, err := loadCfg(&p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir == "" {
		t.Fatal("expected defaults")
	}
}

func TestCmdNewDaemonDown(t *testing.T) {
	apiURLFlag = "http://127.0.0.1:1"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd := cmdNew(&cfg)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "daemon not up") {
		t.Fatalf("want daemon not up, got %v", err)
	}
}

func TestCmdNewWithMockCreate(t *testing.T) {
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
					Name: "sbox-9", Status: vm.StatusRunning, Image: "grain-ubuntu",
					SSHPort: 2209, Persistent: false,
					Forwards: []vm.PortForward{{HostPort: 8080, GuestPort: 80, Proto: "tcp"}},
					Mounts:   []vm.Mount{{Host: "/tmp", Guest: "/mnt"}},
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
	cmd.SetArgs([]string{"--name", "sbox-9", "-c", "2", "-m", "1024", "-P", "8080:80", "-v", "/tmp:/mnt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("new: %v", err)
	}
	if gotBody["name"] != "sbox-9" {
		t.Fatalf("body %+v", gotBody)
	}
}

func TestCmdNewBadPublish(t *testing.T) {
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
	cmd.SetArgs([]string{"-P", "notaport"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected publish parse error")
	}
}

func TestCmdNewBadVolume(t *testing.T) {
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
	cmd.SetArgs([]string{"-v", "nocolon"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected volume parse error")
	}
}

func TestCmdNewBadSocket(t *testing.T) {
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
	cmd.SetArgs([]string{"--publish-socket", "bad"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected socket parse error")
	}
}

func TestCmdXMissingCommand(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1", Status: "running"}})
			return
		}
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdX(&cfg)
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestCmdXExecViaDaemon(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1", Status: "running"}})
		case strings.Contains(r.URL.Path, "/exec"):
			if r.URL.Query().Get("buffered") == "false" {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"type":"started"}` + "\n"))
				_, _ = w.Write([]byte(`{"type":"stdout","data":"hello\n"}` + "\n"))
				_, _ = w.Write([]byte(`{"type":"exit","exit_code":0}` + "\n"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"stdout": "hello\n", "exit_code": 0})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdX(&cfg)
	cmd.SetArgs([]string{"sbox-1", "--", "echo", "hello"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("x: %v", err)
	}
}

func TestCmdXExitCode(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1", Status: "running"}})
		case strings.Contains(r.URL.Path, "/exec"):
			if r.URL.Query().Get("buffered") == "false" {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"type":"exit","exit_code":7}` + "\n"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"exit_code": 7})
		default:
			http.NotFound(w, r)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdX(&cfg)
	cmd.SetArgs([]string{"sbox-1", "--", "false"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected exit code error")
	}
	var ec exitCodeError
	if !asExitCode(err, &ec) || ec.ExitCode() != 7 {
		// exitCodeError may be returned wrapped or as-is
		if !strings.Contains(err.Error(), "exit status 7") {
			t.Fatalf("err=%v", err)
		}
	}
}

func asExitCode(err error, dest *exitCodeError) bool {
	if e, ok := err.(exitCodeError); ok {
		*dest = e
		return true
	}
	return false
}

func TestCmdUpRequiresLocal(t *testing.T) {
	apiURLFlag = "http://10.0.0.8:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_TOKEN", "x")
	cfg := ""
	if err := cmdUp(&cfg).Execute(); err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("%v", err)
	}
	if err := cmdDown(&cfg).Execute(); err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("%v", err)
	}
}

func TestCmdDownBadPid(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	// no pid file
	if err := cmdDown(&cfgPath).Execute(); err == nil {
		t.Fatal("expected not running")
	}
	// bad pid
	if err := os.WriteFile(filepath.Join(dir, "grain.pid"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdDown(&cfgPath).Execute(); err == nil {
		t.Fatal("expected bad pid")
	}
}

func TestCmdLogsMissing(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdLogs(&cfgPath)
	cmd.SetArgs([]string{"nope"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "no serial log") {
		t.Fatalf("%v", err)
	}
}

func TestCmdLogsQEMU(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "sbox-1.log"), []byte("qemu out\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdLogs(&cfgPath)
	cmd.SetArgs([]string{"--qemu", "sbox-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdLogsRequiresLocal(t *testing.T) {
	apiURLFlag = "http://10.0.0.2:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_TOKEN", "t")
	cfg := ""
	cmd := cmdLogs(&cfg)
	cmd.SetArgs([]string{"x"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("%v", err)
	}
}

func TestCmdFwdRmBadPort(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1"}})
			return
		}
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	rm := cmdFwdRm(&cfg)
	rm.SetArgs([]string{"sbox-1", "0"})
	if err := rm.Execute(); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("%v", err)
	}
	rm = cmdFwdRm(&cfg)
	rm.SetArgs([]string{"sbox-1", "abc"})
	if err := rm.Execute(); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestCmdFwdEmptyVMs(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{})
			return
		}
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	if err := cmdFwdLs(&cfg).Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdSecretSetMissingFile(t *testing.T) {
	// fails before daemon if file missing
	cfg := ""
	cmd := cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"tok", "--from-file", filepath.Join(t.TempDir(), "missing")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestIsTruncateAndErr(t *testing.T) {
	t.Parallel()
	if !isTruncate(errTruncate{}) {
		t.Fatal("truncate")
	}
	if isTruncate(fmt.Errorf("other")) {
		t.Fatal("other")
	}
	if (errTruncate{}).Error() != "file truncated" {
		t.Fatal((errTruncate{}).Error())
	}
}

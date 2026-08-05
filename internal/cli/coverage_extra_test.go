package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/update"
	"github.com/cxdy/grain/internal/vm"
)

func TestRunUninstallFullPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "grain-bin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := executablePath
	executablePath = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executablePath = old })

	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(data, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "agent", "x"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		DataDir: data,
		Socket:  filepath.Join(data, "grain.sock"),
		APIURL:  "http://127.0.0.1:9",
	}
	if err := os.WriteFile(cfg.Socket, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runUninstall(cfg, false, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); !os.IsNotExist(err) {
		t.Fatal("binary should be removed")
	}
	if _, err := os.Stat(filepath.Join(data, "agent")); !os.IsNotExist(err) {
		t.Fatal("agent dir should be gone without purge")
	}
}

func TestRunUninstallPurgeYes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "g")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := executablePath
	executablePath = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executablePath = old })

	data := filepath.Join(dir, ".grain")
	if err := os.MkdirAll(filepath.Join(data, "vms"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: data, Socket: filepath.Join(data, "s.sock"), APIURL: "http://127.0.0.1:1"}
	if err := runUninstall(cfg, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Fatal("data should be purged")
	}
}

func TestRunUninstallExecutableError(t *testing.T) {
	old := executablePath
	executablePath = func() (string, error) { return "", fmt.Errorf("no exe") }
	t.Cleanup(func() { executablePath = old })
	cfg := config.Config{DataDir: t.TempDir(), APIURL: "http://127.0.0.1:1"}
	if err := runUninstall(cfg, false, true); err == nil {
		t.Fatal("expected error")
	}
}

func TestRemoveBinaryAlreadyGone(t *testing.T) {
	t.Parallel()
	if err := removeBinary(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatal(err)
	}
	if err := removeBinary(""); err == nil {
		t.Fatal("empty")
	}
}

func TestStopLocalDaemonBestEffort(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, Socket: filepath.Join(dir, "g.sock")}
	stopLocalDaemonBestEffort(cfg)
	if err := os.WriteFile(daemonPIDPath(cfg), []byte("999999991\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stopLocalDaemonBestEffort(cfg)
}

func TestRunUpdateAllBranches(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir

	if displayVer("") != "(unknown)" || displayVer("dev") != "dev" {
		t.Fatal("displayVer")
	}
	if displayVer("v1.2.3") != "v1.2.3" || displayVer("1.2.3") != "v1.2.3" {
		t.Fatal("displayVer semver")
	}

	scriptSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#!/bin/sh\necho ok\n"))
	}))
	t.Cleanup(scriptSrv.Close)
	t.Setenv("GRAIN_INSTALL_SCRIPT", scriptSrv.URL)

	old := checkForUpdate
	t.Cleanup(func() { checkForUpdate = old })

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: opts.Current, Latest: "v9.9.9", UpdateAvail: true, HTMLURL: "https://x"}, nil
	}
	if err := runUpdate(cfg, "0.1.0", true, false); err == nil {
		t.Fatal("want error")
	} else if _, ok := err.(exitCodeError); !ok {
		t.Fatalf("want exitCodeError, got %T %v", err, err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "0.2.0", Latest: "v0.2.0", UpdateAvail: false}, nil
	}
	if err := runUpdate(cfg, "0.2.0", true, false); err != nil {
		t.Fatal(err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "dev", Latest: "v0.2.0", UpdateAvail: false}, nil
	}
	if err := runUpdate(cfg, "dev", true, false); err != nil {
		t.Fatal(err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "1.0.0", Latest: "v1.0.0", UpdateAvail: false}, nil
	}
	if err := runUpdate(cfg, "1.0.0", false, false); err != nil {
		t.Fatal(err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "dev", Latest: "v1.0.0", UpdateAvail: false}, nil
	}
	if err := runUpdate(cfg, "dev", false, false); err != nil {
		t.Fatal(err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "2.0.0", Latest: "v1.0.0", UpdateAvail: false}, nil
	}
	if err := runUpdate(cfg, "2.0.0", false, false); err != nil {
		t.Fatal(err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "1.0.0", Latest: "v1.0.0", UpdateAvail: false}, nil
	}
	if err := runUpdate(cfg, "1.0.0", false, true); err != nil {
		t.Fatal(err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "1.0.0", Latest: "v1.1.0", UpdateAvail: true}, nil
	}
	if err := runUpdate(cfg, "1.0.0", false, false); err != nil {
		t.Fatal(err)
	}

	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{}, fmt.Errorf("network down")
	}
	if err := runUpdate(cfg, "1.0.0", true, false); err == nil {
		t.Fatal("expected check error")
	}
	if err := runUpdate(cfg, "1.0.0", false, false); err != nil {
		t.Fatal(err)
	}

	// cmdUpdate RunE
	cfgPath := filepath.Join(dir, "nope.yaml")
	checkForUpdate = func(opts update.Options) (update.Result, error) {
		return update.Result{Current: "0.0.0", Latest: "v0.0.0", UpdateAvail: false}, nil
	}
	cmd := cmdUpdate(&cfgPath, "0.0.0")
	cmd.SetArgs([]string{"--check"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestRunMCPListenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	_ = os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\napi_url: "+srv.URL+"\n"), 0o644)
	if err := runMCP(&cfgPath, "t", true, "256.1.1.1:9"); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestCmdStatsWithMockDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "only", "status": "running"}})
		case strings.HasSuffix(r.URL.Path, "/stats"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uptime_sec": 1.5, "mem_total_bytes": 100, "mem_available_bytes": 50, "load1": 0.1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	_ = os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\n"), 0o644)
	cmd := cmdStats(&cfgPath)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cmd2 := cmdStats(&cfgPath)
	cmd2.SetArgs([]string{"only"})
	if err := cmd2.Execute(); err != nil {
		t.Fatal(err)
	}
	// loadCfg error: path is a directory → Load fails
	badDir := filepath.Join(dir, "is-a-dir")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd3 := cmdStats(&badDir)
	if err := cmd3.Execute(); err == nil {
		t.Fatal("expected load error for directory config path")
	}
	// daemon health fail
	apiURLFlag = "http://127.0.0.1:1"
	cmd4 := cmdStats(&cfgPath)
	cmd4.SetArgs([]string{"only"})
	if err := cmd4.Execute(); err == nil {
		t.Fatal("expected daemon health error")
	}
}

func TestInstallScriptURL(t *testing.T) {
	t.Setenv("GRAIN_INSTALL_SCRIPT", "http://x/y")
	if installScriptURL() != "http://x/y" {
		t.Fatal(installScriptURL())
	}
	t.Setenv("GRAIN_INSTALL_SCRIPT", "")
	if installScriptURL() != update.DefaultInstallScript {
		t.Fatal(installScriptURL())
	}
}

func TestCmdUninstallExecute(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "grain")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := executablePath
	executablePath = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executablePath = old })

	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(filepath.Join(data, "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("data_dir: "+data+"\napi_url: \"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdUninstall(&cfgPath)
	cmd.SetArgs([]string{"-y"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	// broken config still runs with defaults + our executable hook
	bad := filepath.Join(dir, "missing-cfg.yaml")
	bin2 := filepath.Join(dir, "grain2")
	_ = os.WriteFile(bin2, []byte("y"), 0o755)
	executablePath = func() (string, error) { return bin2, nil }
	cmd2 := cmdUninstall(&bad)
	cmd2.SetArgs([]string{"-y"})
	_ = cmd2.Execute() // may remove defaults paths; just exercise RunE
}

func TestRunUninstallRemoteSkipsDaemonStop(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "g")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := executablePath
	executablePath = func() (string, error) { return bin, nil }
	t.Cleanup(func() { executablePath = old })

	data := filepath.Join(dir, "d")
	_ = os.MkdirAll(filepath.Join(data, "agent"), 0o755)
	// API URL → remoteMode true → skip stopLocalDaemonBestEffort
	cfg := config.Config{
		DataDir: data,
		APIURL:  "http://example.com:9999",
		Socket:  filepath.Join(data, "s.sock"),
	}
	if err := runUninstall(cfg, false, true); err != nil {
		t.Fatal(err)
	}
}

func TestStopLocalDaemonAliveProcess(t *testing.T) {
	// Spawn a short-lived sleep so pidAlive is true and SIGTERM is delivered.
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, Socket: filepath.Join(dir, "g.sock")}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	if err := os.WriteFile(daemonPIDPath(cfg), []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	stopLocalDaemonBestEffort(cfg)
	// Process should be dead or dying.
	time.Sleep(100 * time.Millisecond)
}

func TestRemovePathUnexpectedLabel(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "not-grain-name")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removePath(p, "other"); err == nil {
		t.Fatal("expected refuse unexpected path")
	}
}

func TestRemoveBinaryPermissionError(t *testing.T) {
	// Non-empty directory cannot be removed with os.Remove (needs RemoveAll).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "child"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeBinary(dir); err == nil {
		t.Fatal("expected error removing non-empty directory as binary")
	}
}

func TestRunMCPDefaultsConfig(t *testing.T) {
	// Bad config path falls back to Defaults; dial to dead socket should still error or hang listen.
	// Use httpMode with invalid listen after dial... Dial uses socket from defaults which may not exist
	// but Dial for unix missing path still returns a client.
	// Force listen error after dial via invalid address.
	p := filepath.Join(t.TempDir(), "missing.yaml")
	if err := runMCP(&p, "test", true, "256.9.9.9:1"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdStatusHappyAndBranches(t *testing.T) {
	// Unreachable agent
	srv1 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "s", Status: "running", Image: "grain-ubuntu"}})
		case r.URL.Path == "/vms/s":
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "s", Status: "running", Image: "grain-ubuntu"})
		case strings.HasSuffix(r.URL.Path, "/agent/health"):
			http.Error(w, "down", 502)
		default:
			http.NotFound(w, r)
		}
	})
	cfg1 := withRemoteCfg(t, srv1.URL)
	if err := cmdStatus(&cfg1).Execute(); err != nil {
		t.Fatal(err)
	}

	// Full readiness payload
	srv2 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "s", Status: "running"}})
		case r.URL.Path == "/vms/s":
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "s", Status: "running"})
		case strings.HasSuffix(r.URL.Path, "/agent/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hostname": "g", "userdata_ran": true,
				"readiness": map[string]any{
					"state": "ready", "phase": "boot", "message": "ok", "ready_name": "default",
				},
			})
		default:
			http.NotFound(w, r)
		}
	})
	cfg2 := withRemoteCfg(t, srv2.URL)
	cmd := cmdStatus(&cfg2)
	cmd.SetArgs([]string{"s"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// readiness with error field (no message)
	srv3 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "s", Status: "running"}})
		case r.URL.Path == "/vms/s":
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "s", Status: "running"})
		case strings.HasSuffix(r.URL.Path, "/agent/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hostname": "g", "userdata_ran": false,
				"readiness": map[string]any{"state": "failed", "error": "boom"},
			})
		default:
			http.NotFound(w, r)
		}
	})
	cfg3 := withRemoteCfg(t, srv3.URL)
	if err := cmdStatus(&cfg3).Execute(); err != nil {
		t.Fatal(err)
	}

	// readiness none
	srv4 := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "s", Status: "running"}})
		case r.URL.Path == "/vms/s":
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "s", Status: "running"})
		case strings.HasSuffix(r.URL.Path, "/agent/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{"hostname": "g", "userdata_ran": false})
		default:
			http.NotFound(w, r)
		}
	})
	cfg4 := withRemoteCfg(t, srv4.URL)
	if err := cmdStatus(&cfg4).Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdStatusErrorPaths(t *testing.T) {
	dir := t.TempDir()
	if err := cmdStatus(&dir).Execute(); err == nil {
		t.Fatal("loadCfg")
	}
	cfg := withRemoteCfg(t, "http://example.com:9")
	if err := cmdStatus(&cfg).Execute(); err == nil {
		t.Fatal("auth")
	}
	// get fail
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "s", Status: "running"}})
			return
		}
		http.Error(w, "no", 500)
	})
	cfg2 := withRemoteCfg(t, srv.URL)
	if err := cmdStatus(&cfg2).Execute(); err == nil {
		t.Fatal("get fail")
	}
}

func TestCmdXViaDaemonSuccess(t *testing.T) {
	srv := fullMockDaemon(t)
	cfg := withRemoteCfg(t, srv.URL)
	cmd := cmdX(&cfg)
	cmd.SetArgs([]string{"sbox-1", "--", "uname", "-a"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	// missing command after name
	cmd2 := cmdX(&cfg)
	cmd2.SetArgs([]string{"sbox-1"})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("expected missing command")
	}
	// empty after --
	cmd3 := cmdX(&cfg)
	cmd3.SetArgs([]string{"sbox-1", "--"})
	if err := cmd3.Execute(); err == nil {
		t.Fatal("expected missing after --")
	}
	// mutual exclusive flags
	cmd4 := cmdX(&cfg)
	cmd4.SetArgs([]string{"--ssh", "--agent", "sbox-1", "--", "true"})
	if err := cmd4.Execute(); err == nil {
		t.Fatal("expected flag conflict")
	}
}

func TestCmdShViaDaemonAgent(t *testing.T) {
	// fullMockDaemon has no websocket shell — expect error, but covers remote force path.
	srv := fullMockDaemon(t)
	cfg := withRemoteCfg(t, srv.URL)
	cmd := cmdSh(&cfg)
	cmd.SetArgs([]string{"sbox-1", "--agent"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected shell error against mock")
	}
}

func TestRootPersistentPreRun(t *testing.T) {
	// Exercise PersistentPreRun via a nested command with isolated config.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	_ = os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\ncheck_updates: false\n"), 0o644)
	root := Root("test-ver")
	root.SetArgs([]string{"--config", cfgPath, "version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestRunDoctorQemuDefaultAndX86Binary(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		DataDir:    dir,
		Hypervisor: "qemu",
		Image:      "grain-ubuntu",
		// empty QEMUBinary → arch-specific default
		QEMUBinary: "",
	}
	_ = runDoctor(cfg)

	// firecracker with empty FirecrackerBinary name + missing kernel path default
	cfg2 := config.Config{
		DataDir:            dir,
		Hypervisor:         "firecracker",
		FirecrackerBinary:  "",
		Image:              "grain-ubuntu",
		KernelPath:         "", // uses default under data_dir
	}
	_ = runDoctor(cfg2)
}

func TestKvmNestedVirtHintAndCheckDevKVM(t *testing.T) {
	// On macOS /dev/kvm missing
	old := kvmDevicePath
	t.Cleanup(func() { kvmDevicePath = old })
	kvmDevicePath = filepath.Join(t.TempDir(), "no-kvm")
	if err := checkDevKVM(); err == nil {
		t.Fatal("expected missing kvm")
	}
	// path is a directory
	kvmDevicePath = t.TempDir()
	if err := checkDevKVM(); err == nil {
		t.Fatal("expected dir error")
	}
	// regular file not char device — open may fail or succeed depending on OS
	f := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	kvmDevicePath = f
	_ = checkDevKVM()

	// hint with fake cpuinfo
	oldCPU := cpuinfoPath
	t.Cleanup(func() { cpuinfoPath = oldCPU })
	cpuinfoPath = filepath.Join(t.TempDir(), "missing")
	if kvmNestedVirtHint() != "" {
		t.Fatal("missing cpuinfo")
	}
	// hypervisor without nested virt
	p := filepath.Join(t.TempDir(), "cpuinfo")
	_ = os.WriteFile(p, []byte("processor : 0\nflags : fpu hypervisor sse\n"), 0o644)
	cpuinfoPath = p
	if h := kvmNestedVirtHint(); !strings.Contains(h, "nested") {
		t.Fatalf("want nested hint, got %q", h)
	}
	// no nest flags at all
	_ = os.WriteFile(p, []byte("flags : fpu sse\n"), 0o644)
	if h := kvmNestedVirtHint(); !strings.Contains(h, "vmx/svm") {
		t.Fatalf("want bios hint, got %q", h)
	}
	// has nest — empty
	_ = os.WriteFile(p, []byte("flags : fpu vmx sse\n"), 0o644)
	if h := kvmNestedVirtHint(); h != "" {
		t.Fatalf("want empty, got %q", h)
	}
	// features line (arm style)
	_ = os.WriteFile(p, []byte("Features : fp asimd svm\n"), 0o644)
	if h := kvmNestedVirtHint(); h != "" {
		t.Fatalf("svm present: %q", h)
	}
}

func TestCmdStatsClientFromAuthFail(t *testing.T) {
	// Hit clientFrom error path in stats (line 23-25).
	cfg := withRemoteCfg(t, "http://example.com:9")
	if err := cmdStats(&cfg).Execute(); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestCmdStatsResolveFail(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{
				{Name: "a", Status: "running"},
				{Name: "b", Status: "running"},
			})
			return
		}
		http.NotFound(w, r)
	})
	cfg := withRemoteCfg(t, srv.URL)
	// no args + multi VM → resolve error
	if err := cmdStats(&cfg).Execute(); err == nil {
		t.Fatal("expected resolve error")
	}
}

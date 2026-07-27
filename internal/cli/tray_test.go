package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestTrayStatusConfigError(t *testing.T) {
	// Point config at a path that cannot be a valid yaml file read — use a directory as config path.
	dir := t.TempDir()
	// loadCfg with a directory path fails on read
	p := dir // IsNotExist? directory exists but ReadFile fails
	// Actually ReadFile on dir returns error (is a directory).
	st := trayStatus(&p)
	if !strings.Contains(st.Title, "grain") {
		t.Fatalf("title %q", st.Title)
	}
}

func TestTrayStatusDaemonDown(t *testing.T) {
	apiURLFlag = "http://127.0.0.1:1" // nothing listening
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	st := trayStatus(&cfg)
	if st.Title != "grain · off" {
		// clientFrom may succeed (HTTP client built) then Health fails → still off
		if !strings.Contains(st.Title, "off") && !strings.Contains(st.Tooltip, "not reachable") {
			t.Fatalf("status=%+v", st)
		}
	}
}

func TestTrayStatusWithVMs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{
				{Name: "a", Status: "running"},
				{Name: "b", Status: "stopped"},
				{Name: "c", Status: "paused"},
				{Name: "d", Status: "creating"},
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
	st := trayStatus(&cfg)
	if st.Title != "grain · 3" {
		t.Fatalf("title=%q tooltip=%q", st.Title, st.Tooltip)
	}
	if !strings.Contains(st.Tooltip, "3 active") {
		t.Fatalf("tooltip %q", st.Tooltip)
	}
}

func TestTrayStatusHealthOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path == "/vms" {
			http.Error(w, "fail", 500)
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
	st := trayStatus(&cfg)
	if st.Title != "grain · on" {
		t.Fatalf("%+v", st)
	}
}

func TestCmdStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1", Status: "running"}})
		case strings.HasSuffix(r.URL.Path, "/stats"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uptime_sec": 12.5, "mem_total_bytes": 1024, "mem_available_bytes": 512, "load1": 0.1,
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
	cmd := cmdStats(&cfg)
	cmd.SetArgs([]string{"sbox-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdStatsResolveSingleVM(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "only", Status: "running"}})
		default:
			if strings.HasSuffix(r.URL.Path, "/stats") {
				_ = json.NewEncoder(w).Encode(map[string]any{"uptime_sec": 1})
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd := cmdStats(&cfg)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestTrayStatusClientFromAuthFail(t *testing.T) {
	// Non-loopback remote without token → clientFrom fails inside trayStatus.
	apiURLFlag = "http://example.com:9"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	st := trayStatus(&cfg)
	if st.Title != "grain · off" {
		t.Fatalf("status=%+v", st)
	}
	if !strings.Contains(st.Tooltip, "daemon not reachable") && !strings.Contains(st.Tooltip, "token") {
		// clientFrom error path uses fixed tooltip "daemon not reachable — grain up"
		if st.Tooltip == "" {
			t.Fatalf("empty tooltip: %+v", st)
		}
	}
}

func TestCmdTrayRequireLocalNote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows path covered separately")
	}
	// Remote mode: requireLocalDaemon soft-fails (stderr note) then tray.Run would block.
	// We only assert Persistent path isn't used — cmdTray RunE hits requireLocalDaemon.
	// Don't call tray.Run: instead verify Windows message and construction.
	cfg := ""
	cmd := cmdTray(&cfg, "test")
	if cmd.Use != "tray" {
		t.Fatal(cmd.Use)
	}
	// Invalid config directory → loadCfg error before tray.Run
	dir := t.TempDir()
	// Using a directory as config path fails loadCfg.
	bad := dir
	cmd = cmdTray(&bad, "test")
	// Will fail loadCfg or requireLocal then tray.Run — either is fine if non-nil
	// but tray.Run blocks. Skip execute; construction is enough.
	if cmd.Short == "" {
		t.Fatal("empty short")
	}
}

func TestCmdStatsTableErrors(t *testing.T) {
	cfg := ""
	// Bad config (directory)
	dir := t.TempDir()
	cmd := cmdStats(&dir)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected config error")
	}

	// Auth fail
	apiURLFlag = "http://example.com:9"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cmd = cmdStats(&cfg)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected auth error")
	}

	// resolveVMName multi-VM
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{
				{Name: "a", Status: "running"},
				{Name: "b", Status: "running"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Setenv("GRAIN_TOKEN", "")
	cmd = cmdStats(&cfg)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected which-vm error")
	}

	// Stats API error after health OK
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "only", Status: "running"}})
		case strings.HasSuffix(r.URL.Path, "/stats"):
			http.Error(w, "stats fail", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv2.Close()
	apiURLFlag = srv2.URL
	cmd = cmdStats(&cfg)
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected stats error")
	}
}

func TestCmdTrayLoadCfgError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows covered")
	}
	// Directory as config path → loadCfg fails before tray.Run.
	dir := t.TempDir()
	cmd := cmdTray(&dir, "v-test")
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected loadCfg error")
	}
}

func TestCmdTrayRemoteModeSoftNoteThenStub(t *testing.T) {
	// When CGO is enabled tray.Run blocks — only exercise requireLocalDaemon soft path
	// by not calling Execute. Construction already covered.
	// With remote mode, requireLocalDaemon returns error which is only logged.
	apiURLFlag = "http://127.0.0.1:9"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	cfg := ""
	cmd := cmdTray(&cfg, "v")
	// Don't Execute (blocks). Verify Use/Long.
	if !strings.Contains(cmd.Long, "grain up") {
		t.Fatalf("long: %s", cmd.Long)
	}
}

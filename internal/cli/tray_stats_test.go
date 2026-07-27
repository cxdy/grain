package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

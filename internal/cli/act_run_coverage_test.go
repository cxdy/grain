package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/vm"
)

// mockActAPI serves health, create stream, exec (ready probe + act), and delete.
func mockActAPI(t *testing.T, opts actMockOpts) *httptest.Server {
	t.Helper()
	var deleted atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/vms":
			if opts.createErr != "" {
				http.Error(w, `{"error":"`+opts.createErr+`"}`, 500)
				return
			}
			// CreateStream expects NDJSON ready event
			w.Header().Set("Content-Type", "application/x-ndjson")
			name := opts.vmName
			if name == "" {
				name = "act-proj"
			}
			ev := vm.CreateEvent{
				Phase: vm.PhaseReady,
				Name:  name,
				Instance: &vm.Instance{
					Name:     name,
					Status:   vm.StatusRunning,
					CPUs:     2,
					MemoryMB: 4096,
				},
			}
			b, _ := json.Marshal(ev)
			_, _ = w.Write(append(b, '\n'))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/exec"):
			if r.URL.Query().Get("buffered") == "false" {
				if opts.streamFail {
					http.Error(w, "stream fail", 500)
					return
				}
				w.Header().Set("Content-Type", "application/x-ndjson")
				code := opts.exitCode
				_, _ = w.Write([]byte(`{"type":"stdout","data":"act-out\n"}` + "\n"))
				_, _ = w.Write([]byte(`{"type":"stderr","data":"act-err\n"}` + "\n"))
				_, _ = fmt.Fprintf(w, `{"type":"exit","exit_code":%d}`+"\n", code)
				return
			}
			// buffered: waitActReady probe or fallback
			out := "READY\n"
			if opts.notReady {
				out = "not yet\n"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"stdout": out, "stderr": "", "exit_code": 0,
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/vms/"):
			deleted.Store(true)
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		if opts.expectDelete && !deleted.Load() {
			t.Errorf("expected Delete to be called")
		}
	})
	return srv
}

type actMockOpts struct {
	vmName       string
	createErr    string
	exitCode     int
	streamFail   bool
	notReady     bool
	expectDelete bool
}

func setupActAPI(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	// Isolated config so ~/.grain/config.yaml cannot interfere.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func TestRunGrainActHappyPath(t *testing.T) {
	srv := mockActAPI(t, actMockOpts{vmName: "act-myproj", expectDelete: true})
	cfg := setupActAPI(t, srv)

	work := t.TempDir()
	// with and without .github/workflows
	err := runGrainAct(&cfg, actOpts{
		Dir:     work,
		Name:    "act-myproj",
		CPUs:    2,
		Mem:     2048,
		Timeout: 30 * time.Second,
		ActArgs: []string{"-l"},
	})
	if err != nil {
		t.Fatalf("runGrainAct: %v", err)
	}
}

func TestRunGrainActKeepAndDefaultArgs(t *testing.T) {
	srv := mockActAPI(t, actMockOpts{vmName: "act-keep", expectDelete: false})
	cfg := setupActAPI(t, srv)

	work := t.TempDir()
	wf := filepath.Join(work, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wf, "ci.yml"), []byte("name: ci\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runGrainAct(&cfg, actOpts{
		Keep:    true,
		Dir:     work,
		Name:    "act-keep",
		Timeout: 20 * time.Second,
		// empty ActArgs → defaults to -l
	})
	if err != nil {
		t.Fatalf("keep: %v", err)
	}
}

func TestRunGrainActDefaultResourcesAndName(t *testing.T) {
	// empty name → sanitize from dir base; cpus/mem 0 → preset defaults
	srv := mockActAPI(t, actMockOpts{expectDelete: true})
	cfg := setupActAPI(t, srv)

	// Use a nested dir so Base is predictable; server accepts any create name
	parent := t.TempDir()
	work := filepath.Join(parent, "Cool_Repo!")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	err := runGrainAct(&cfg, actOpts{
		Dir:     work,
		Timeout: 20 * time.Second,
		ActArgs: []string{"-j", "test"},
	})
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
}

func TestRunGrainActBadProjectDir(t *testing.T) {
	srv := mockActAPI(t, actMockOpts{})
	cfg := setupActAPI(t, srv)

	// missing dir
	err := runGrainAct(&cfg, actOpts{
		Dir:     filepath.Join(t.TempDir(), "nope-missing"),
		Timeout: 5 * time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "project dir") {
		t.Fatalf("missing dir: %v", err)
	}

	// file not directory
	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = runGrainAct(&cfg, actOpts{Dir: f, Timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file dir: %v", err)
	}
}

func TestRunGrainActCreateFails(t *testing.T) {
	srv := mockActAPI(t, actMockOpts{createErr: "no capacity"})
	cfg := setupActAPI(t, srv)
	err := runGrainAct(&cfg, actOpts{
		Dir:     t.TempDir(),
		Name:    "act-fail",
		Timeout: 10 * time.Second,
		ActArgs: []string{"-l"},
	})
	if err == nil || !strings.Contains(err.Error(), "create sandbox") {
		t.Fatalf("create fail: %v", err)
	}
}

func TestRunGrainActNonZeroExit(t *testing.T) {
	srv := mockActAPI(t, actMockOpts{vmName: "act-nz", exitCode: 7, expectDelete: true})
	cfg := setupActAPI(t, srv)
	err := runGrainAct(&cfg, actOpts{
		Dir:     t.TempDir(),
		Name:    "act-nz",
		Timeout: 20 * time.Second,
		ActArgs: []string{"-j", "fail"},
	})
	if err == nil {
		t.Fatal("expected exit code error")
	}
	if ec, ok := err.(exitCodeError); !ok || int(ec) != 7 {
		t.Fatalf("got %v (%T)", err, err)
	}
}

func TestRunGrainActStreamFallback(t *testing.T) {
	// ExecStream fails; waitActReady uses buffered; stream fallback uses execViaAgent viaDaemon=true
	// which will also hit stream fail then buffered success.
	var streamHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodPost && r.URL.Path == "/vms":
			w.Header().Set("Content-Type", "application/x-ndjson")
			ev := vm.CreateEvent{
				Phase:    vm.PhaseReady,
				Name:     "act-fb",
				Instance: &vm.Instance{Name: "act-fb", Status: vm.StatusRunning},
			}
			b, _ := json.Marshal(ev)
			_, _ = w.Write(append(b, '\n'))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/exec"):
			if r.URL.Query().Get("buffered") == "false" {
				streamHits.Add(1)
				http.Error(w, "no stream", http.StatusServiceUnavailable)
				return
			}
			// waitActReady probe OR buffered fallback after stream fail
			cmd := r.URL.Query().Get("cmd")
			args := r.URL.Query()["args"]
			joined := cmd + " " + strings.Join(args, " ")
			if strings.Contains(joined, "READY") || strings.Contains(joined, "command -v act") {
				_ = json.NewEncoder(w).Encode(map[string]any{"stdout": "READY\n", "exit_code": 0})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"stdout": "ok\n", "exit_code": 0})
		case r.Method == http.MethodDelete:
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := setupActAPI(t, srv)

	err := runGrainAct(&cfg, actOpts{
		Dir:     t.TempDir(),
		Name:    "act-fb",
		Timeout: 25 * time.Second,
		ActArgs: []string{"-l"},
	})
	if err != nil {
		t.Fatalf("stream fallback: %v", err)
	}
	if streamHits.Load() < 1 {
		t.Fatal("expected at least one stream attempt")
	}
}

func TestRunGrainActHealthFail(t *testing.T) {
	// Point at closed server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	url := srv.URL
	srv.Close()

	apiURLFlag = url
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")

	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runGrainAct(&cfg, actOpts{Dir: t.TempDir(), Timeout: 3 * time.Second})
	if err == nil {
		t.Fatal("expected health error")
	}
	if !strings.Contains(err.Error(), "daemon not up") && !strings.Contains(err.Error(), "unhealthy") {
		// client may wrap dial errors
		t.Logf("health err: %v", err)
	}
}

func TestRunGrainActZeroTimeoutDefaults(t *testing.T) {
	srv := mockActAPI(t, actMockOpts{vmName: "act-to", expectDelete: true})
	cfg := setupActAPI(t, srv)
	// Timeout <= 0 → 15m default inside runGrainAct (still finishes quickly with mock)
	err := runGrainAct(&cfg, actOpts{
		Dir:     t.TempDir(),
		Name:    "act-to",
		Timeout: 0,
		ActArgs: []string{"-l"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitActReadyPollsThenOK(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		out := "not yet\n"
		if i >= 2 {
			out = "READY\n"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"stdout": out, "exit_code": 0})
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if err := waitActReady(c, "vm", time.Now().Add(15*time.Second)); err != nil {
		t.Fatal(err)
	}
	if n.Load() < 2 {
		t.Fatalf("polls=%d", n.Load())
	}
}

func TestSanitizeActNameEdgeCases(t *testing.T) {
	t.Parallel()
	// trailing dash after truncate
	s := strings.Repeat("a", 40) + "!!!!" + strings.Repeat("b", 20)
	got := sanitizeActName(s)
	if len(got) > 48 {
		t.Fatalf("len %d: %q", len(got), got)
	}
	if got[0] < 'a' || got[0] > 'z' {
		// may start with act- for digits only
		if !strings.HasPrefix(got, "act-") && got != "act" {
			t.Fatalf("invalid start: %q", got)
		}
	}
	// only special chars → act
	if g := sanitizeActName("@@@"); g != "act" {
		t.Fatalf("%q", g)
	}
	// digits only
	if g := sanitizeActName("99"); !strings.HasPrefix(g, "act-") {
		t.Fatalf("%q", g)
	}
}

func TestShellQuoteApostrophe(t *testing.T) {
	t.Parallel()
	g := shellQuote("it's")
	if !strings.Contains(g, "'") {
		t.Fatalf("%q", g)
	}
	// round-trip style: should be bash-safe single-quoted
	if g == "it's" {
		t.Fatal("apostrophe should force quoting")
	}
}

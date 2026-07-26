package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func testServer(t *testing.T) *api.Server {
	t.Helper()
	s, _ := testServerWithStore(t)
	return s
}

func testServerWithStore(t *testing.T) (*api.Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := manager.New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	return api.New(mgr, observability.NewMetrics(), nil), st
}

// startLocalAgent boots a grain-agent on 127.0.0.1:0 and returns its host port.
func startLocalAgent(t *testing.T) int {
	t.Helper()
	srv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && addr != "127.0.0.1:0" && !strings.HasSuffix(addr, ":0") {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, err = strconv.Atoi(p)
				if err == nil && port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("agent did not bind a port")
	}

	// Confirm health.
	ac := &agent.Client{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		HTTP:    &http.Client{Timeout: 2 * time.Second},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := agent.Wait(ctx, ac); err != nil {
		t.Fatalf("wait for agent: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	return port
}

func createMockVM(t *testing.T, h http.Handler, name string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"name":%q,"persistent":false}`, name))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
}

func setAgentPort(t *testing.T, st *store.Store, name string, port int) {
	t.Helper()
	inst, err := st.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	inst.AgentPort = port
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
}

func TestHealthAndCreateListDelete(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != 200 {
		t.Fatalf("health %d", rr.Code)
	}

	body := []byte(`{"persistent":false}`)
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
	var inst map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &inst); err != nil {
		t.Fatal(err)
	}
	name, _ := inst["name"].(string)
	if name == "" {
		t.Fatal("no name")
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/"+name, nil))
	if rr.Code != 200 {
		t.Fatalf("delete %d %s", rr.Code, rr.Body.String())
	}
}

func TestShutdownAndStartPersistent(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	body := []byte(`{"name":"lab","persistent":true}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/shutdown", nil))
	if rr.Code != 200 {
		t.Fatalf("shutdown %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/lab", nil))
	if rr.Code != 200 {
		t.Fatalf("get after shutdown %d %s", rr.Code, rr.Body.String())
	}
	var stopped map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped["status"] != "stopped" {
		t.Fatalf("want stopped, got %v", stopped["status"])
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/start", nil))
	if rr.Code != 200 {
		t.Fatalf("start %d %s", rr.Code, rr.Body.String())
	}
	var started map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started["status"] != "running" {
		t.Fatalf("want running, got %v", started["status"])
	}
}


func TestCreateStreamNDJSON(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	body := []byte(`{"name":"stream1","persistent":false}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?stream=1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("stream create %d %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "ndjson") && !strings.Contains(ct, "json") {
		t.Fatalf("content-type %q", ct)
	}

	var phases []string
	var readyName string
	sc := bufio.NewScanner(bytes.NewReader(rr.Body.Bytes()))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev vm.CreateEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		phases = append(phases, ev.Phase)
		if ev.Phase == vm.PhaseReady {
			readyName = ev.Name
			if ev.Instance == nil && ev.Name == "" {
				t.Fatal("ready event missing name/instance")
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(phases) < 6 {
		t.Fatalf("want >=6 events, got %v", phases)
	}
	if phases[len(phases)-1] != vm.PhaseReady {
		t.Fatalf("last phase %v", phases)
	}
	if readyName != "stream1" {
		t.Fatalf("ready name %q", readyName)
	}
	// confirm image..ready order contains required phases
	joined := strings.Join(phases, ",")
	for _, p := range []string{vm.PhaseImage, vm.PhaseDisk, vm.PhaseSeed, vm.PhaseQEMU, vm.PhaseWaitSSH, vm.PhaseReady} {
		if !strings.Contains(joined, p) {
			t.Fatalf("missing phase %s in %v", p, phases)
		}
	}
}

func TestExecViaAgent(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)

	createMockVM(t, h, "exec1")
	setAgentPort(t, st, "exec1", agentPort)

	// Success: echo hello
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/exec1/exec?cmd=echo&args=hello", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("exec %d %s", rr.Code, rr.Body.String())
	}
	var res agent.ExecResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit_code %d error=%q stderr=%q", res.ExitCode, res.Error, res.Stderr)
	}
	if strings.TrimSpace(res.Stdout) != "hello" {
		t.Fatalf("stdout %q", res.Stdout)
	}

	// Non-zero exit still HTTP 200
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/exec1/exec?cmd=/bin/sh&args=-c&args=exit+42", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("exec nonzero %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 42 {
		t.Fatalf("exit_code %d want 42", res.ExitCode)
	}

	// Missing cmd → 400
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/exec1/exec", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing cmd want 400 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestExecAgentUnavailable(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()

	createMockVM(t, h, "noagent")
	setAgentPort(t, st, "noagent", 0)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/noagent/exec?cmd=echo&args=hi", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "agent not available") {
		t.Fatalf("body %s", rr.Body.String())
	}

	// Agent port set but nothing listening → 502
	setAgentPort(t, st, "noagent", 1) // 127.0.0.1:1 refused
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/noagent/exec?cmd=echo&args=hi", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("want 502 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestAgentHealthProxy(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)

	createMockVM(t, h, "health1")
	setAgentPort(t, st, "health1", agentPort)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/health1/agent/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health %d %s", rr.Code, rr.Body.String())
	}
	var health agent.Health
	if err := json.Unmarshal(rr.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.AgentVersion != agent.Version {
		t.Fatalf("agent_version %q", health.AgentVersion)
	}

	// No agent port → 503
	setAgentPort(t, st, "health1", 0)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/health1/agent/health", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestAPIClientExecAndAgentHealth(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	agentPort := startLocalAgent(t)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	createMockVM(t, s.Handler(), "cli1")
	setAgentPort(t, st, "cli1", agentPort)

	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	h, err := c.AgentHealth(ctx, "cli1")
	if err != nil {
		t.Fatalf("AgentHealth: %v", err)
	}
	if h.AgentVersion == "" {
		t.Fatal("empty AgentVersion")
	}

	res, err := c.Exec(ctx, "cli1", "echo", "from-client")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 || strings.TrimSpace(res.Stdout) != "from-client" {
		t.Fatalf("result %+v", res)
	}
}

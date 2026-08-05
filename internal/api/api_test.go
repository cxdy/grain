package api_test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
	"github.com/cxdy/grain/internal/secrets"
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
	srv := api.New(mgr, observability.NewMetrics(), nil)
	sec, err := secrets.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv.Secrets = sec
	return srv, st
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

func TestAgentDeployEndpoint(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()

	// missing VM → 404
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/nope/agent/deploy", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d %s", rr.Code, rr.Body.String())
	}

	createMockVM(t, h, "dep1")
	inst, err := st.Get("dep1")
	if err != nil {
		t.Fatal(err)
	}
	// mock create leaves running with SSH; strip SSH to hit 400
	inst.SSHPort = 0
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/dep1/agent/deploy", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 no ssh got %d %s", rr.Code, rr.Body.String())
	}

	// restore SSH; no agent binary → 503
	inst.SSHPort = 2201
	inst.IP = "127.0.0.1"
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/dep1/agent/deploy", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 binary missing got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "agent binary") {
		t.Fatalf("body: %s", rr.Body.String())
	}

	// plant binary under data dir → deploy attempts SSH and fails with 502
	dataDir := filepath.Dir(filepath.Dir(st.Dir("dep1")))
	agentDir := filepath.Join(dataDir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"grain-agent-linux-amd64", "grain-agent-linux-arm64"} {
		if err := os.WriteFile(filepath.Join(agentDir, name), []byte("#!/bin/true\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/dep1/agent/deploy", nil))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("want 502 scp fail got %d %s", rr.Code, rr.Body.String())
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

func TestExecStreamNDJSON(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)

	createMockVM(t, h, "stream-exec")
	setAgentPort(t, st, "stream-exec", agentPort)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/stream-exec/exec?cmd=echo&args=stream-hello&buffered=false", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("stream exec %d %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "ndjson") {
		t.Fatalf("content-type %q want ndjson", ct)
	}

	var types []string
	var stdout strings.Builder
	var exitCode int
	gotExit := false
	sc := bufio.NewScanner(bytes.NewReader(rr.Body.Bytes()))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var frame agent.ExecFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("frame %q: %v", line, err)
		}
		types = append(types, frame.Type)
		if frame.Type == "stdout" {
			stdout.WriteString(frame.Data)
		}
		if frame.Type == "exit" {
			gotExit = true
			if frame.ExitCode != nil {
				exitCode = *frame.ExitCode
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !gotExit {
		t.Fatalf("frames %v: missing exit", types)
	}
	if exitCode != 0 {
		t.Fatalf("exit_code %d", exitCode)
	}
	if strings.TrimSpace(stdout.String()) != "stream-hello" {
		t.Fatalf("stdout %q", stdout.String())
	}
	if len(types) < 2 || types[0] != "started" || types[len(types)-1] != "exit" {
		t.Fatalf("frames %v", types)
	}

	// Non-zero exit still streams exit frame
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/stream-exec/exec?cmd=/bin/sh&args=-c&args=exit+7&buffered=false", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("stream nonzero %d %s", rr.Code, rr.Body.String())
	}
	gotExit = false
	exitCode = -1
	sc = bufio.NewScanner(bytes.NewReader(rr.Body.Bytes()))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var frame agent.ExecFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("frame: %v", err)
		}
		if frame.Type == "exit" && frame.ExitCode != nil {
			gotExit = true
			exitCode = *frame.ExitCode
		}
	}
	if !gotExit || exitCode != 7 {
		t.Fatalf("want exit 7, gotExit=%v code=%d body=%s", gotExit, exitCode, rr.Body.String())
	}

	// No agent → 503
	setAgentPort(t, st, "stream-exec", 0)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/stream-exec/exec?cmd=echo&args=x&buffered=false", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestPutGetFileViaAPI(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)

	createMockVM(t, h, "cp1")
	setAgentPort(t, st, "cp1", agentPort)

	dir := t.TempDir()
	guestPath := filepath.Join(dir, "sub", "hello.txt")
	payload := []byte("hello via daemon api\n")

	// PUT binary
	rr := httptest.NewRecorder()
	u := "/vms/cp1/cp?path=" + url.QueryEscape(guestPath) + "&mode=binary&permissions=0644"
	req := httptest.NewRequest(http.MethodPut, u, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(payload))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("put %d %s", rr.Code, rr.Body.String())
	}

	got, err := os.ReadFile(guestPath)
	if err != nil {
		t.Fatalf("disk read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("disk %q want %q", got, payload)
	}

	// GET binary
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/cp1/cp?path="+url.QueryEscape(guestPath)+"&mode=binary", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get %d %s", rr.Code, rr.Body.String())
	}
	if !bytes.Equal(rr.Body.Bytes(), payload) {
		t.Fatalf("get body %q want %q", rr.Body.Bytes(), payload)
	}

	// Missing path → 400
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/vms/cp1/cp?mode=binary", bytes.NewReader(payload))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing path want 400 got %d", rr.Code)
	}

	// Not found → 404
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/cp1/cp?path="+url.QueryEscape(filepath.Join(dir, "missing"))+"&mode=binary", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d %s", rr.Code, rr.Body.String())
	}

	// No agent → 503
	setAgentPort(t, st, "cp1", 0)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/cp1/cp?path="+url.QueryEscape(guestPath), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d", rr.Code)
	}
}

func TestFSReadDirStatMkdirRemoveViaAPI(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)

	createMockVM(t, h, "fs1")
	setAgentPort(t, st, "fs1", agentPort)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mkdir nested
	nested := filepath.Join(root, "a", "b")
	body, _ := json.Marshal(agent.MkdirRequest{Path: nested, Recursive: true, Mode: "0755"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/fs1/fs/mkdir", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("mkdir %d %s", rr.Code, rr.Body.String())
	}

	// Stat dir
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/fs1/fs/stat?path="+url.QueryEscape(nested), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("stat %d %s", rr.Code, rr.Body.String())
	}
	var info agent.FSInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Type != "directory" {
		t.Fatalf("stat type %q", info.Type)
	}
	if info.Name != "b" {
		t.Fatalf("stat name %q", info.Name)
	}

	// Put a file under nested via cp, then readdir
	child := filepath.Join(nested, "c.txt")
	rr = httptest.NewRecorder()
	putURL := "/vms/fs1/cp?path=" + url.QueryEscape(child) + "&mode=binary"
	req = httptest.NewRequest(http.MethodPut, putURL, strings.NewReader("data"))
	req.ContentLength = 4
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("put child %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/fs1/fs/readdir?path="+url.QueryEscape(nested), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("readdir %d %s", rr.Code, rr.Body.String())
	}
	var entries []agent.FSInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.Name == "c.txt" {
			found = true
			if e.Type != "file" {
				t.Fatalf("c.txt type %q", e.Type)
			}
		}
	}
	if !found {
		t.Fatalf("readdir missing c.txt: %+v", entries)
	}

	// Remove recursive tree
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/vms/fs1/fs/remove?path="+url.QueryEscape(filepath.Join(root, "a"))+"&recursive=true", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("remove %d %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Fatalf("nested still exists: %v", err)
	}

	// Stat missing → 404
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/fs1/fs/stat?path="+url.QueryEscape(nested), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d %s", rr.Code, rr.Body.String())
	}

	// Missing path → 400
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/fs1/fs/readdir", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rr.Code)
	}

	// No agent → 503
	setAgentPort(t, st, "fs1", 0)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/fs1/fs/stat?path="+url.QueryEscape(root), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d", rr.Code)
	}
}

func TestAPIClientStreamCPAndFS(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	agentPort := startLocalAgent(t)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	createMockVM(t, s.Handler(), "cli2")
	setAgentPort(t, st, "cli2", agentPort)

	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	// ExecStream
	var stdout strings.Builder
	code, err := c.ExecStream(ctx, "cli2", agent.ExecOpts{Cmd: "echo", Args: []string{"cli-stream"}}, func(f agent.ExecFrame) error {
		if f.Type == "stdout" {
			stdout.WriteString(f.Data)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ExecStream: %v", err)
	}
	if code != 0 || strings.TrimSpace(stdout.String()) != "cli-stream" {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}

	// PutFile + GetFile
	dir := t.TempDir()
	guestPath := filepath.Join(dir, "f.txt")
	payload := []byte("client-cp")
	if err := c.PutFile(ctx, "cli2", guestPath, bytes.NewReader(payload), int64(len(payload)), agent.CPOpts{Mode: "0644"}); err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, "cli2", guestPath, &buf); err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("GetFile %q", buf.Bytes())
	}

	// Mkdir / Stat / ReadDir / Remove
	nested := filepath.Join(dir, "n1", "n2")
	if err := c.Mkdir(ctx, "cli2", nested, true, "0755"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	stInfo, err := c.Stat(ctx, "cli2", nested)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stInfo.Type != "directory" {
		t.Fatalf("type %q", stInfo.Type)
	}
	entries, err := c.ReadDir(ctx, "cli2", dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("empty readdir")
	}
	if err := c.Remove(ctx, "cli2", filepath.Join(dir, "n1"), true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestCreateWaitQueryModes(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	for _, mode := range []string{"ssh", "agent", "userdata"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"name":"w-%s","persistent":false}`, mode))
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/vms?wait="+mode+"&stream=1", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/x-ndjson")
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d %s", rr.Code, rr.Body.String())
			}
			var phases []string
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
			}
			if len(phases) == 0 || phases[len(phases)-1] != vm.PhaseReady {
				t.Fatalf("phases %v", phases)
			}
			joined := strings.Join(phases, ",")
			switch mode {
			case "ssh":
				if !strings.Contains(joined, vm.PhaseWaitSSH) {
					t.Fatalf("want wait_ssh in %v", phases)
				}
			case "agent":
				if !strings.Contains(joined, vm.PhaseWaitAgent) {
					t.Fatalf("want wait_agent in %v", phases)
				}
			case "userdata":
				if !strings.Contains(joined, vm.PhaseUserdata) {
					t.Fatalf("want userdata in %v", phases)
				}
			}
		})
	}
}

func TestCreateWaitInvalid(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?wait=nope", bytes.NewReader([]byte(`{"name":"bad"}`)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid wait mode") {
		t.Fatalf("body %s", rr.Body.String())
	}
}

func TestCreateTimeoutInvalid(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?timeout=notaduration", bytes.NewReader([]byte(`{"name":"t"}`)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestCreateClientWaitQuery(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go http.Serve(ln, s.Handler()) //nolint:errcheck

	c := &api.Client{Base: "http://" + ln.Addr().String()}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var phases []string
	inst, err := c.CreateStream(ctx, api.CreateRequest{
		Name: "cli-wait",
		Wait: vm.WaitAgent,
	}, func(ev vm.CreateEvent) {
		phases = append(phases, ev.Phase)
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "cli-wait" {
		t.Fatalf("name %s", inst.Name)
	}
	if !strings.Contains(strings.Join(phases, ","), vm.PhaseWaitAgent) {
		t.Fatalf("phases %v", phases)
	}
}

func TestPauseResumeAPI(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	createMockVM(t, h, "lab")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/pause", nil))
	if rr.Code != 200 {
		t.Fatalf("pause %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/lab", nil))
	var inst map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &inst); err != nil {
		t.Fatal(err)
	}
	if inst["status"] != "paused" {
		t.Fatalf("status %v", inst["status"])
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/resume", nil))
	if rr.Code != 200 {
		t.Fatalf("resume %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/lab", nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &inst); err != nil {
		t.Fatal(err)
	}
	if inst["status"] != "running" {
		t.Fatalf("status %v", inst["status"])
	}
}

func TestSuspendRestoreAPI(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	// Persistent VM required for suspend
	body := []byte(`{"name":"lab","persistent":true}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/suspend", nil))
	if rr.Code != 200 {
		t.Fatalf("suspend %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/lab", nil))
	var inst map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &inst); err != nil {
		t.Fatal(err)
	}
	if inst["status"] != "suspended" {
		t.Fatalf("status %v", inst["status"])
	}
	if inst["suspended_at"] == nil || inst["suspended_at"] == "" {
		t.Fatal("expected suspended_at")
	}

	// start while suspended should fail
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/start", nil))
	if rr.Code == 200 {
		t.Fatal("expected start reject while suspended")
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/restore", nil))
	if rr.Code != 200 {
		t.Fatalf("restore %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &inst); err != nil {
		t.Fatal(err)
	}
	if inst["status"] != "running" {
		t.Fatalf("status %v", inst["status"])
	}
}

func TestLiveForwardAPI(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	createMockVM(t, h, "svc")

	body := []byte(`{"host_port":18080,"guest_port":80}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/svc/forwards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add forward %d %s", rr.Code, rr.Body.String())
	}
	var lf map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &lf); err != nil {
		t.Fatal(err)
	}
	if lf["host_port"] != float64(18080) || lf["guest_port"] != float64(80) {
		t.Fatalf("lf %+v", lf)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/svc/forwards/18080", nil))
	if rr.Code != 200 {
		t.Fatalf("rm forward %d %s", rr.Code, rr.Body.String())
	}
}

func TestSecretsAPI(t *testing.T) {
	srv := testServer(t)
	// POST
	body := `{"name":"api-tok","data_base64":"c2VjcmV0","mode":"0600"}`
	req := httptest.NewRequest(http.MethodPost, "/secrets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body.String())
	}
	// LIST
	req = httptest.NewRequest(http.MethodGet, "/secrets", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status %d", w.Code)
	}
	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/secrets/api-tok", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", w.Code, w.Body.String())
	}
}

func TestVMStatsAPI(t *testing.T) {
	srv, st := testServerWithStore(t)
	port := startLocalAgent(t)
	// Seed a running-ish VM with agent port.
	inst := &vm.Instance{
		Name:      "statsvm",
		Status:    vm.StatusRunning,
		AgentPort: port,
		CPUs:      1,
		MemoryMB:  512,
	}
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/vms/statsvm/stats", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stats status %d: %s", w.Code, w.Body.String())
	}
	var stBody agent.Stats
	if err := json.NewDecoder(w.Body).Decode(&stBody); err != nil {
		t.Fatal(err)
	}
	// On darwin without /proc, fields may be zero; just ensure JSON shape.
	_ = stBody
}

func TestInfoAndMetrics(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/info", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("info %d", rr.Code)
	}
	var info map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info["name"] != "grain" || info["version"] == "" {
		t.Fatalf("info %+v", info)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("metrics %d", rr.Code)
	}
}

func TestNewNilDeps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := manager.New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	s := api.New(mgr, nil, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != 200 {
		t.Fatalf("health %d", rr.Code)
	}
}

func TestCreateWaitFalseRejected(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?wait=false", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestCreateWaitTrueLegacyAndTimeout(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?wait=true&timeout=30s", strings.NewReader(`{"name":"legacy","persistent":false}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestGetVMNotFound(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestSecretsNotConfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := manager.New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	s := api.New(mgr, observability.NewMetrics(), nil)
	// Secrets left nil
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secrets", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
}

func TestSecretsGetPatch(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	body := `{"name":"gp","data_base64":"c2Vj","mode":"0600"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secrets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secrets/gp", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secrets/gp?data=1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get data %d", rr.Code)
	}
	var sec map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &sec); err != nil {
		t.Fatal(err)
	}
	if sec["data_base64"] == nil || sec["data_base64"] == "" {
		t.Fatalf("want data_base64: %+v", sec)
	}

	rr = httptest.NewRecorder()
	patch := `{"mode":"0640"}`
	req = httptest.NewRequest(http.MethodPatch, "/secrets/gp", strings.NewReader(patch))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("patch %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secrets/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing %d", rr.Code)
	}
}

func TestInjectSecretAPI(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)

	// Create host secret
	body := `{"name":"inj","data_base64":"aGVsbG8=","mode":"0600"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secrets", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create secret %d %s", rr.Code, rr.Body.String())
	}

	createMockVM(t, h, "injvm")
	setAgentPort(t, st, "injvm", agentPort)

	guestPath := filepath.Join(t.TempDir(), "secret.txt")
	injBody, _ := json.Marshal(map[string]string{"path": guestPath})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/injvm/secrets/inj", bytes.NewReader(injBody))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("inject %d %s", rr.Code, rr.Body.String())
	}
	var out agent.MaterializeSecretResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Path != guestPath {
		t.Fatalf("path %q", out.Path)
	}
	got, err := os.ReadFile(guestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Fatalf("file %q", got)
	}

	// Missing secret
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/injvm/secrets/nope", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", rr.Code)
	}

	// No agent
	setAgentPort(t, st, "injvm", 0)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/injvm/secrets/inj", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestAPIClientHealthStatsSecrets(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	agentPort := startLocalAgent(t)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	createMockVM(t, s.Handler(), "cli3")
	setAgentPort(t, st, "cli3", agentPort)

	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
	list, err := c.List(ctx)
	if err != nil || len(list) == 0 {
		t.Fatalf("List: %v %v", err, list)
	}

	stStats, err := c.Stats(ctx, "cli3")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	_ = stStats

	meta, err := c.SetSecret(ctx, secrets.PutRequest{Name: "csec", DataBase64: "eHg=", Mode: "0600"})
	if err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if meta.Name != "csec" {
		t.Fatalf("meta %+v", meta)
	}
	secs, err := c.ListSecrets(ctx)
	if err != nil || len(secs) == 0 {
		t.Fatalf("ListSecrets: %v %v", err, secs)
	}

	guestPath := filepath.Join(t.TempDir(), "s")
	inj, err := c.InjectSecret(ctx, "cli3", "csec", guestPath)
	if err != nil {
		t.Fatalf("InjectSecret: %v", err)
	}
	if inj.Path != guestPath {
		t.Fatalf("path %q", inj.Path)
	}
	if err := c.DeleteSecret(ctx, "csec"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}

	// Lifecycle via client
	if err := c.Pause(ctx, "cli3"); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if err := c.Resume(ctx, "cli3"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := c.Shutdown(ctx, "cli3"); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestAPIClientLifecyclePersistent(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	inst, err := c.Create(ctx, api.CreateRequest{Name: "pers", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Suspend(ctx, inst.Name); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	restored, err := c.Restore(ctx, inst.Name)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.Status != vm.StatusRunning {
		t.Fatalf("status %s", restored.Status)
	}
	lf, err := c.AddForward(ctx, inst.Name, 0, 8080)
	if err != nil {
		t.Fatalf("AddForward: %v", err)
	}
	if err := c.RemoveForward(ctx, inst.Name, lf.HostPort); err != nil {
		t.Fatalf("RemoveForward: %v", err)
	}
	if err := c.Shutdown(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}
	started, err := c.Start(ctx, inst.Name)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != vm.StatusRunning {
		t.Fatalf("status %s", started.Status)
	}
	if err := c.Delete(ctx, inst.Name); err != nil {
		t.Fatal(err)
	}
}

func TestAPIClientPutGetTar(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	agentPort := startLocalAgent(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	createMockVM(t, s.Handler(), "tar1")
	setAgentPort(t, st, "tar1", agentPort)

	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	if err := c.PutTar(ctx, "tar1", "", strings.NewReader("x")); err == nil {
		t.Fatal("expected empty path")
	}
	if err := c.GetTar(ctx, "tar1", "", io.Discard); err == nil {
		t.Fatal("expected empty path")
	}

	// Minimal tar archive with one file.
	dir := t.TempDir()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	hdr := &tar.Header{Name: "hello.txt", Mode: 0o644, Size: 5}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	extractTo := filepath.Join(dir, "extract")
	if err := os.MkdirAll(extractTo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := c.PutTar(ctx, "tar1", extractTo, bytes.NewReader(tarBuf.Bytes())); err != nil {
		t.Fatalf("PutTar: %v", err)
	}
	// GetTar of the extracted file or dir
	var out bytes.Buffer
	if err := c.GetTar(ctx, "tar1", extractTo, &out); err != nil {
		t.Fatalf("GetTar: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("expected tar bytes")
	}
}

func TestAddForwardBadJSONAndMissingGuest(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	createMockVM(t, h, "fwd")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/fwd/forwards", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/fwd/forwards", strings.NewReader(`{"host_port":1}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing guest %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/fwd/forwards/notanint", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad hostPort %d", rr.Code)
	}
}

func TestExecWithUIDGIDCwd(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)
	createMockVM(t, h, "execopts")
	setAgentPort(t, st, "execopts", agentPort)

	rr := httptest.NewRecorder()
	u := "/vms/execopts/exec?cmd=echo&args=hi&uid=0&gid=0&cwd=/tmp"
	req := httptest.NewRequest(http.MethodPost, u, nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("exec %d %s", rr.Code, rr.Body.String())
	}

	// invalid uid
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/execopts/exec?cmd=echo&uid=nope", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad uid %d", rr.Code)
	}
}

func TestCPModeTarAndInvalid(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	agentPort := startLocalAgent(t)
	createMockVM(t, h, "cpmode")
	setAgentPort(t, st, "cpmode", agentPort)

	// invalid mode
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/vms/cpmode/cp?path=/tmp/x&mode=zip", strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad mode put %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms/cpmode/cp?path=/tmp/x&mode=zip", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad mode get %d", rr.Code)
	}

	// tar put via API
	dir := t.TempDir()
	extractTo := filepath.Join(dir, "ex")
	if err := os.MkdirAll(extractTo, 0o755); err != nil {
		t.Fatal(err)
	}
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	_ = tw.WriteHeader(&tar.Header{Name: "a.txt", Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("a"))
	_ = tw.Close()
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/vms/cpmode/cp?path="+url.QueryEscape(extractTo)+"&mode=tar", bytes.NewReader(tarBuf.Bytes()))
	req.Header.Set("Content-Type", "application/x-tar")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("put tar %d %s", rr.Code, rr.Body.String())
	}

	// invalid uid on put
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/vms/cpmode/cp?path="+url.QueryEscape(filepath.Join(dir, "b"))+"&mode=binary&uid=x", strings.NewReader("z"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad uid %d", rr.Code)
	}
}

func TestPutCPMaxBody(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	// Agent not required: Content-Length over limit is rejected before proxying.
	createMockVM(t, h, "putmax")
	setAgentPort(t, st, "putmax", 1)

	u := "/vms/putmax/cp?path=" + url.QueryEscape("/tmp/out.bin") + "&mode=binary"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, u, strings.NewReader("x"))
	req.ContentLength = agent.DefaultMaxPutBytes + 1
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over limit: %d %s", rr.Code, rr.Body.String())
	}
}

func TestShellAgentUnavailable(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()
	createMockVM(t, h, "sh1")
	setAgentPort(t, st, "sh1", 0)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/sh1/shell", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestCreateSecretInvalidJSON(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secrets", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestCreateVMInvalidJSON(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	// Malformed JSON → 400
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON want 400 got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid JSON body") {
		t.Fatalf("body %s", rr.Body.String())
	}

	// Not JSON at all → 400
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("non-JSON want 400 got %d %s", rr.Code, rr.Body.String())
	}

	// Empty body is allowed (defaults)
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("empty body want 201 got %d %s", rr.Code, rr.Body.String())
	}
	var inst map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &inst); err != nil {
		t.Fatal(err)
	}
	name, _ := inst["name"].(string)
	if name == "" {
		t.Fatal("expected default name")
	}
}

func TestInjectSecretInvalidJSON(t *testing.T) {
	t.Parallel()
	s, st := testServerWithStore(t)
	h := s.Handler()

	_, err := s.Secrets.Put(secrets.PutRequest{
		Name:       "badjson",
		DataBase64: "aGVsbG8=",
	})
	if err != nil {
		t.Fatal(err)
	}
	createMockVM(t, h, "inj-badjson")
	// No agent needed: decode happens before agent dial.
	setAgentPort(t, st, "inj-badjson", 0)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/inj-badjson/secrets/badjson", strings.NewReader(`{"path":`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON want 400 got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid JSON body") {
		t.Fatalf("body %s", rr.Body.String())
	}
}

func TestCreateAcceptNDJSONHeader(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	body := []byte(`{"name":"accept-nd","persistent":false}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("stream via Accept %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "ndjson") {
		t.Fatalf("ct %q", rr.Header().Get("Content-Type"))
	}
}

// --- merged from api_more_test.go / coverage_boost_test.go ---

func TestAgentUnavailableRoutes(t *testing.T) {
	s, st := testServerWithStore(t)
	h := s.Handler()
	createMockVM(t, h, "errvm")
	setAgentPort(t, st, "errvm", 0)

	paths := []struct {
		method, url string
		body        string
	}{
		{http.MethodPost, "/vms/errvm/exec?cmd=true", ""},
		{http.MethodPut, "/vms/errvm/cp?path=/tmp/x", "x"},
		{http.MethodGet, "/vms/errvm/cp?path=/tmp/x", ""},
		{http.MethodGet, "/vms/errvm/fs/readdir?path=/", ""},
		{http.MethodGet, "/vms/errvm/fs/stat?path=/", ""},
		{http.MethodPost, "/vms/errvm/fs/mkdir", `{"path":"/tmp/a"}`},
		{http.MethodDelete, "/vms/errvm/fs/remove?path=/tmp/a", ""},
		{http.MethodGet, "/vms/errvm/agent/health", ""},
		{http.MethodGet, "/vms/errvm/stats", ""},
	}
	for _, p := range paths {
		rr := httptest.NewRecorder()
		var body io.Reader
		if p.body != "" {
			body = strings.NewReader(p.body)
		}
		req := httptest.NewRequest(p.method, p.url, body)
		if p.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusOK || rr.Code == http.StatusNoContent {
			t.Fatalf("%s %s: unexpected success %d", p.method, p.url, rr.Code)
		}
	}
}

func TestCPAndFSAndExecValidation(t *testing.T) {
	s, st := testServerWithStore(t)
	h := s.Handler()
	port := startLocalAgent(t)
	createMockVM(t, h, "val1")
	setAgentPort(t, st, "val1", port)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/vms/val1/cp", strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cp path: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/vms/val1/cp?path=/tmp/x&mode=weird", strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cp mode: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/vms/val1/cp?path=/tmp/x&uid=bad", strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("uid: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/vms/val1/cp?path=/tmp/x&gid=bad", strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("gid: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/val1/cp", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("get cp path: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/val1/cp?path=/tmp/x&mode=weird", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("get mode: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/val1/fs/readdir", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("readdir: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/val1/fs/stat", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("stat: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/val1/fs/mkdir", strings.NewReader("{"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("mkdir json: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/val1/fs/mkdir", strings.NewReader(`{"path":""}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("mkdir path: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/val1/fs/remove", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("remove: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/val1/exec", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("exec cmd: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/val1/exec?cmd=true&uid=xx", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("exec uid: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/val1/exec?cmd=true&gid=yy", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("exec gid: %d", rr.Code)
	}
}

func TestSecretsDeleteNotFoundAndInject(t *testing.T) {
	s, st := testServerWithStore(t)
	h := s.Handler()
	createMockVM(t, h, "secvm")
	setAgentPort(t, st, "secvm", 0)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/secrets/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("delete: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secrets/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/secrets/nope", strings.NewReader(`{"mode":"0600"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("patch: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/secvm/secrets/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("inject missing secret: %d %s", rr.Code, rr.Body.String())
	}

	_, err := s.Secrets.Put(secrets.PutRequest{
		Name:       "tok",
		DataBase64: "dG9r",
	})
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/secvm/secrets/tok", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("inject no agent: %d %s", rr.Code, rr.Body.String())
	}

	// inject success with live agent
	port := startLocalAgent(t)
	setAgentPort(t, st, "secvm", port)
	guestPath := filepath.Join(t.TempDir(), "injected")
	body, _ := json.Marshal(map[string]string{"path": guestPath})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/secvm/secrets/tok", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("inject ok: %d %s", rr.Code, rr.Body.String())
	}
}

func TestAPIClientCreateStreamAndLifecycle(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	inst, err := c.CreateStream(ctx, api.CreateRequest{Name: "stream-1", Persistent: false}, func(ev vm.CreateEvent) {
		_ = ev.Phase
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst == nil || inst.Name == "" {
		t.Fatalf("%+v", inst)
	}

	got, err := c.Get(ctx, inst.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != inst.Name {
		t.Fatalf("%+v", got)
	}

	if _, err := c.Get(ctx, "no-such-vm-xyz"); err == nil {
		t.Fatal("expected get error")
	}
	if err := c.Delete(ctx, "no-such-vm-xyz"); err == nil {
		t.Fatal("expected delete error")
	}

	// CreateStream with nil onEvent
	inst2, err := c.CreateStream(ctx, api.CreateRequest{Name: "stream-2", Persistent: false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inst2 == nil {
		t.Fatal("nil inst")
	}
}

func TestAPIClientExecStreamAndPutGet(t *testing.T) {
	s, st := testServerWithStore(t)
	port := startLocalAgent(t)
	h := s.Handler()
	createMockVM(t, h, "ex1")
	setAgentPort(t, st, "ex1", port)

	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	if _, err := c.Exec(ctx, "ex1", ""); err == nil {
		t.Fatal("empty cmd")
	}
	if _, err := c.ExecStream(ctx, "ex1", agent.ExecOpts{}, func(agent.ExecFrame) error { return nil }); err == nil {
		t.Fatal("empty cmd stream")
	}
	if _, err := c.ExecStream(ctx, "ex1", agent.ExecOpts{Cmd: "echo"}, nil); err == nil {
		t.Fatal("nil onFrame")
	}

	code, err := c.ExecStream(ctx, "ex1", agent.ExecOpts{Cmd: "echo", Args: []string{"hi"}}, func(f agent.ExecFrame) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("code %d", code)
	}

	// Put/Get file via client
	guest := filepath.Join(t.TempDir(), "f.txt")
	payload := []byte("hello-api")
	if err := c.PutFile(ctx, "ex1", guest, bytes.NewReader(payload), int64(len(payload)), agent.CPOpts{Mode: "0644"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, "ex1", guest, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != string(payload) {
		t.Fatalf("%q", buf.String())
	}

	// validation
	if err := c.PutFile(ctx, "ex1", "", bytes.NewReader(payload), 1, agent.CPOpts{}); err == nil {
		t.Fatal("empty path")
	}
	if err := c.GetFile(ctx, "ex1", "", io.Discard); err == nil {
		t.Fatal("empty get")
	}
	if err := c.PutTar(ctx, "ex1", "", bytes.NewReader(nil)); err == nil {
		t.Fatal("empty put tar")
	}
	if err := c.GetTar(ctx, "ex1", "", io.Discard); err == nil {
		t.Fatal("empty get tar")
	}
	if _, err := c.ReadDir(ctx, "ex1", ""); err == nil {
		t.Fatal("empty readdir")
	}
	if _, err := c.Stat(ctx, "ex1", ""); err == nil {
		t.Fatal("empty stat")
	}
	if err := c.Mkdir(ctx, "ex1", "", false, ""); err == nil {
		t.Fatal("empty mkdir")
	}
	if err := c.Remove(ctx, "ex1", "", false); err == nil {
		t.Fatal("empty remove")
	}
}

func TestFSNotFoundViaAPI(t *testing.T) {
	s, st := testServerWithStore(t)
	port := startLocalAgent(t)
	h := s.Handler()
	createMockVM(t, h, "fsnf")
	setAgentPort(t, st, "fsnf", port)

	missing := filepath.Join(t.TempDir(), "nope-xyz")
	q := url.QueryEscape(missing)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/fsnf/fs/stat?path="+q, nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("stat: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/fsnf/fs/readdir?path="+q, nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("readdir: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/fsnf/cp?path="+q, nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cp: %d", rr.Code)
	}
}

func TestShellProxyToAgent(t *testing.T) {
	s, st := testServerWithStore(t)
	port := startLocalAgent(t)
	h := s.Handler()
	createMockVM(t, h, "sh2")
	setAgentPort(t, st, "sh2", port)

	// Non-upgrade GET: on non-linux agent returns 501; proxy may return 501 or error.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/sh2/shell?cols=80&rows=24", nil))
	// Should not be 404
	if rr.Code == http.StatusNotFound {
		t.Fatalf("shell missing: %d %s", rr.Code, rr.Body.String())
	}
}

func TestCreateStreamInvalidWait(t *testing.T) {
	s := testServer(t)
	body := []byte(`{"name":"badwait","persistent":false}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?stream=1&wait=nope", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code == http.StatusOK || rr.Code == http.StatusCreated {
		t.Fatalf("invalid wait should fail: %d %s", rr.Code, rr.Body.String())
	}
}

func TestAddForwardInvalidHostPort(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	createMockVM(t, h, "fwd1")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/fwd1/forwards/0", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("hostPort 0: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/fwd1/forwards/abc", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("hostPort abc: %d", rr.Code)
	}
}

func TestClientTokenRoundTrip(t *testing.T) {
	s := testServer(t)
	s.APIToken = "secret-token"
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// without token fails
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	if err := c.Health(context.Background()); err != nil {
		t.Logf("health: %v", err)
	}
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("list without token should fail")
	}

	c.Token = "secret-token"
	list, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = list
}

func TestAPICreateStreamErrorPhase(t *testing.T) {
	t.Parallel()
	s, _ := testServerWithStore(t)
	h := s.Handler()

	// Fail create via invalid name to force error phase on stream.
	rr := httptest.NewRecorder()
	body := []byte(`{"name":"Bad_Name"}`)
	req := httptest.NewRequest(http.MethodPost, "/vms?stream=1&wait=ssh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	h.ServeHTTP(rr, req)
	// Stream path writes 200 then NDJSON error events when flusher is available.
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("code %d body %s", rr.Code, rr.Body.String())
	}
	if rr.Code == http.StatusOK && !strings.Contains(rr.Body.String(), "error") && !strings.Contains(rr.Body.String(), "invalid") {
		t.Logf("stream body: %s", rr.Body.String())
	}
}

func TestAPICreateWithAllBodyFields(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	host := t.TempDir()
	body, _ := json.Marshal(map[string]any{
		"name":       "full1",
		"persistent": true,
		"cpus":       1,
		"memory_mb":  256,
		"disk_gb":    2,
		"image":      "ubuntu-cloud",
		"arch":       "amd64",
		"gpu":        "virtio",
		"network":    "slirp",
		"tags":       map[string]string{"k": "v"},
		"userdata":   "#cloud-config\n",
		"forwards":   []map[string]int{{"host_port": 0, "guest_port": 8080}},
		"mounts":     []map[string]string{{"host": host, "guest": "/mnt"}},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?wait=ssh&timeout=2s", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestAPILifecycleErrorPaths(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	createMockVM(t, h, "life1")

	// pause/resume/suspend/restore success and failures
	for _, path := range []string{
		"/vms/missing/pause",
		"/vms/missing/resume",
		"/vms/missing/suspend",
		"/vms/missing/restore",
		"/vms/missing/start",
		"/vms/missing/shutdown",
	} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, path, nil))
		if rr.Code == http.StatusOK {
			t.Fatalf("%s unexpected ok", path)
		}
	}

	// pause then resume
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/life1/pause", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("pause %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/life1/resume", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("resume %d %s", rr.Code, rr.Body.String())
	}

	// shutdown persistent — recreate as persistent
	body := []byte(`{"name":"pers1","persistent":true}`)
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create pers %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/pers1/suspend", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("suspend %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/pers1/restore", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("restore %d %s", rr.Code, rr.Body.String())
	}
}

func TestAPIForwardRemoveInvalidPort(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	createMockVM(t, h, "fwd1")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/fwd1/forwards/0", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("%d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/fwd1/forwards/abc", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("%d", rr.Code)
	}

	// add then remove
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/fwd1/forwards", strings.NewReader(`{"host_port":19091,"guest_port":80}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("add %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/fwd1/forwards/19091", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("rm %d %s", rr.Code, rr.Body.String())
	}
}

func TestAPIExecAndCPWithAgent(t *testing.T) {
	s, st := testServerWithStore(t)
	h := s.Handler()
	port := startLocalAgent(t)
	createMockVM(t, h, "ag1")
	setAgentPort(t, st, "ag1", port)

	// exec buffered with uid/gid/cwd
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms/ag1/exec?cmd=echo&args=hi&uid=0&gid=0&cwd=/", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("exec %d %s", rr.Code, rr.Body.String())
	}

	// exec stream
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/ag1/exec?cmd=echo&args=hi&buffered=false", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("stream %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "exit") && !strings.Contains(rr.Body.String(), "started") {
		t.Logf("stream body %s", rr.Body.String())
	}

	// put/get binary
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/vms/ag1/cp?path=/tmp/grain-api-cov&mode=binary&permissions=0644&uid=0&gid=0", strings.NewReader("hello"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("put %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/ag1/cp?path=/tmp/grain-api-cov&mode=binary", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "hello" {
		t.Fatalf("get %d %q", rr.Code, rr.Body.String())
	}

	// get missing
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/ag1/cp?path=/tmp/no-such-grain-file&mode=binary", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing %d", rr.Code)
	}

	// agent health + stats
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/ag1/agent/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/ag1/stats", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("stats %d", rr.Code)
	}

	// fs ops
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/ag1/fs/mkdir", strings.NewReader(`{"path":"/tmp/grain-api-dir","recursive":true,"mode":"0755"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("mkdir %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/ag1/fs/readdir?path=/tmp", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("readdir %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/ag1/fs/stat?path=/tmp/grain-api-dir", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("stat %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/vms/ag1/fs/remove?path=/tmp/grain-api-dir&recursive=true", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("rm %d", rr.Code)
	}
}

func TestAPIInjectSecretWithAgent(t *testing.T) {
	s, st := testServerWithStore(t)
	h := s.Handler()
	port := startLocalAgent(t)
	createMockVM(t, h, "secvm")
	setAgentPort(t, st, "secvm", port)

	// create secret
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secrets", strings.NewReader(`{"name":"tok","data_base64":"c2VjcmV0","mode":"0600"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("secret %d %s", rr.Code, rr.Body.String())
	}

	// inject with path
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/secvm/secrets/tok", strings.NewReader(`{"path":"/tmp/grain-secret-inj"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("inject %d %s", rr.Code, rr.Body.String())
	}

	// inject missing secret
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/secvm/secrets/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing secret %d", rr.Code)
	}
}

func TestAPISecretsPatchAndGetData(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secrets", strings.NewReader(`{"name":"p1","data_base64":"YQ==","mode":"0600"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secrets/p1?data=1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/secrets/p1", strings.NewReader(`{"mode":"0640"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("patch %d %s", rr.Code, rr.Body.String())
	}

	// patch missing
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/secrets/missing", strings.NewReader(`{"mode":"0640"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("patch miss %d", rr.Code)
	}

	// get missing
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/secrets/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("get miss %d", rr.Code)
	}
}

func TestAPIWriteAgentFSErrNotFound(t *testing.T) {
	s, st := testServerWithStore(t)
	h := s.Handler()
	port := startLocalAgent(t)
	createMockVM(t, h, "fserr")
	setAgentPort(t, st, "fserr", port)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/fserr/fs/stat?path=/tmp/grain-no-such-path-xyz", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestAPIExecInvalidUIDGID(t *testing.T) {
	s, st := testServerWithStore(t)
	h := s.Handler()
	port := startLocalAgent(t)
	createMockVM(t, h, "uidvm")
	setAgentPort(t, st, "uidvm", port)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/uidvm/exec?cmd=true&uid=xx", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("uid %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/uidvm/exec?cmd=true&gid=yy", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("gid %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/uidvm/exec", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cmd %d", rr.Code)
	}
}

func TestAPIOpenAPIAndMetricsAndInfo(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	for _, u := range []string{"/openapi.yaml", "/openapi.json", "/metrics", "/info", "/healthz"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, u, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s %d", u, rr.Code)
		}
	}
}

func TestAPICreateWaitModesViaQuery(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	for i, wait := range []string{"ssh", "agent", "userdata", "auto", "true"} {
		name := fmt.Sprintf("wqm%d", i)
		rr := httptest.NewRecorder()
		body := []byte(`{"name":"` + name + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/vms?wait="+wait+"&timeout=1s", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(rr, req)
		// mock hypervisor short-circuits all wait modes successfully
		if rr.Code != http.StatusCreated {
			t.Fatalf("wait=%s code=%d %s", wait, rr.Code, rr.Body.String())
		}
	}
	// invalid wait
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vms?wait=nope", strings.NewReader(`{"name":"badw"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("%d", rr.Code)
	}
	// invalid timeout
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms?timeout=nope", strings.NewReader(`{"name":"badt"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("%d", rr.Code)
	}
}

func TestAPIShellUnavailable(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	createMockVM(t, h, "shell0")
	// AgentPort from mock may be set; zero it via store
	s2, st := testServerWithStore(t)
	h2 := s2.Handler()
	createMockVM(t, h2, "shell1")
	setAgentPort(t, st, "shell1", 0)
	rr := httptest.NewRecorder()
	h2.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/vms/shell1/shell", nil))
	if rr.Code == http.StatusOK {
		t.Fatalf("expected unavailable, got %d", rr.Code)
	}
	_ = time.Second
	_ = s
	_ = h
}

func TestCloneVMAPI(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()
	body := `{"name":"lab","persistent":true,"forwards":[{"host_port":0,"guest_port":80}]}`
	req := httptest.NewRequest(http.MethodPost, "/vms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/shutdown", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("shutdown %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/lab/clone", strings.NewReader(`{"name":"lab2"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("clone %d %s", rr.Code, rr.Body.String())
	}
	var inst vm.Instance
	if err := json.NewDecoder(rr.Body).Decode(&inst); err != nil {
		t.Fatal(err)
	}
	if inst.Name != "lab2" || inst.Status != vm.StatusStopped || !inst.Persistent {
		t.Fatalf("%+v", inst)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/lab/clone", strings.NewReader(`{"name":"lab2"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("conflict want 409 got %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/vms/lab/start", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("start %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/lab/clone", strings.NewReader(`{"name":"lab3"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("running clone want 400 got %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/vms/nope/clone", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing want 404 got %d", rr.Code)
	}
}

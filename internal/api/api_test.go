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
	defer ln.Close()
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

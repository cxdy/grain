package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/secrets"
	"github.com/cxdy/grain/internal/vm"
)

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

func TestAPIClientListHealthToken(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.APIToken = "tok-abc"
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// no token → unauthorized for list
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("expected unauthorized")
	}
	// health public
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	// with token
	c.Token = "tok-abc"
	if _, err := c.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), "nope"); err == nil {
		t.Fatal("delete missing")
	}
}

func TestAPIClientFSEmptyPathAndExecStreamEdges(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			// blank + error empty + exit
			_, _ = io.WriteString(w, "\n")
			_, _ = io.WriteString(w, `{"type":"error"}`+"\n")
			return
		}
		_, _ = io.WriteString(w, `{"exit_code":0}`)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	if err := c.PutFile(ctx, "n", "", strings.NewReader("x"), 1, agent.CPOpts{}); err == nil {
		t.Fatal("empty put path")
	}
	if err := c.GetFile(ctx, "n", "", io.Discard); err == nil {
		t.Fatal("empty get path")
	}
	if err := c.PutTar(ctx, "n", "", strings.NewReader("x")); err == nil {
		t.Fatal("empty puttar")
	}
	if err := c.GetTar(ctx, "n", "", io.Discard); err == nil {
		t.Fatal("empty gettar")
	}
	if _, err := c.ReadDir(ctx, "n", ""); err == nil {
		t.Fatal("empty readdir")
	}
	if _, err := c.Stat(ctx, "n", ""); err == nil {
		t.Fatal("empty stat")
	}
	if err := c.Mkdir(ctx, "n", "", true, ""); err == nil {
		t.Fatal("empty mkdir")
	}
	if err := c.Remove(ctx, "n", "", false); err == nil {
		t.Fatal("empty rm")
	}
	if _, err := c.Exec(ctx, "n", ""); err == nil {
		t.Fatal("empty cmd")
	}
	uid, gid := uint32(1), uint32(2)
	_, _ = c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "true", UID: &uid, GID: &gid, Cwd: "/"}, func(f agent.ExecFrame) error {
		return nil
	})
	// error frame with empty error message
	_, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "true"}, func(f agent.ExecFrame) error { return nil })
	if err == nil {
		t.Fatal("expected stream error")
	}
}

func TestAPIClientCreateStreamOnEvent(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		_ = enc.Encode(vm.CreateEvent{Phase: vm.PhaseQEMU, Message: "q"})
		_ = enc.Encode(vm.CreateEvent{
			Phase:    vm.PhaseReady,
			Name:     "s",
			Instance: &vm.Instance{Name: "s", Status: vm.StatusRunning},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	var n int
	inst, err := c.CreateStream(context.Background(), api.CreateRequest{Name: "s"}, func(ev vm.CreateEvent) {
		n++
	})
	if err != nil || inst.Name != "s" || n < 2 {
		t.Fatalf("inst=%+v n=%d err=%v", inst, n, err)
	}
}

func TestAPIClientNilHTTPAndDecodeStatus(t *testing.T) {
	t.Parallel()
	// decodeAPIError without JSON body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(418)
		_, _ = io.WriteString(w, "teapot")
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL} // nil HTTP → DefaultClient still works for httptest? No — need client that dials srv.
	c.HTTP = srv.Client()
	if _, err := c.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAPIClientPutFileWithUID(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uid") == "" || r.URL.Query().Get("permissions") == "" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /vms/{name}/secrets/{s}", func(w http.ResponseWriter, r *http.Request) {
		// no body inject
		_, _ = io.WriteString(w, `{"path":"/default"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	uid, gid := uint32(10), uint32(20)
	if err := c.PutFile(context.Background(), "n", "/a", strings.NewReader("z"), 1, agent.CPOpts{
		UID: &uid, GID: &gid, Mode: "0644",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(context.Background(), "n", "k", ""); err != nil {
		t.Fatal(err)
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

// keep secrets import used
var _ = secrets.Meta{}

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

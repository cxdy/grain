package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/secrets"
	"github.com/cxdy/grain/internal/vm"
)

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

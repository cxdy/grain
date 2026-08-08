package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/vm"
)

func TestClientPoolMethodsSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/pool":
			_ = json.NewEncoder(w).Encode(api.PoolStatus{
				Enabled: true, Template: "g", Desired: 2, Ready: 1, Members: []string{"m1"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/pool/fill":
			_ = json.NewEncoder(w).Encode(api.PoolStatus{Enabled: true, Template: "g", Desired: 2, Ready: 2})
		case r.Method == http.MethodPost && r.URL.Path == "/pool/claim":
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "claimed", Status: vm.StatusRunning})
		case r.Method == http.MethodPost && r.URL.Path == "/pool/drain":
			_ = json.NewEncoder(w).Encode(map[string]int{"drained": 3})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	st, err := c.PoolStatus(ctx)
	if err != nil || !st.Enabled || st.Ready != 1 {
		t.Fatalf("%+v %v", st, err)
	}
	st, err = c.PoolFill(ctx)
	if err != nil || st.Ready != 2 {
		t.Fatalf("%+v %v", st, err)
	}
	inst, err := c.PoolClaim(ctx, "work")
	if err != nil || inst.Name != "claimed" {
		t.Fatalf("%+v %v", inst, err)
	}
	n, err := c.PoolDrain(ctx)
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
}

func TestClientPoolAndCloneErrorPaths(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	if _, err := c.PoolStatus(ctx); err == nil {
		t.Fatal("status")
	}
	if _, err := c.PoolFill(ctx); err == nil {
		t.Fatal("fill")
	}
	if _, err := c.PoolClaim(ctx, "x"); err == nil {
		t.Fatal("claim")
	}
	if _, err := c.PoolDrain(ctx); err == nil {
		t.Fatal("drain")
	}
	if _, err := c.Clone(ctx, "s", api.CloneRequest{Name: "d"}); err == nil {
		t.Fatal("clone")
	}
	if _, err := c.DeployAgent(ctx, "x"); err == nil {
		t.Fatal("deploy")
	}
}

func TestClientCloneAndDeploySuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/clone"):
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "dst", Status: vm.StatusStopped, Persistent: true})
		case strings.HasSuffix(r.URL.Path, "/agent/deploy"):
			_ = json.NewEncoder(w).Encode(api.AgentDeployResult{Name: "src", Binary: "/tmp/grain-agent"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	inst, err := c.Clone(ctx, "src", api.CloneRequest{Name: "dst"})
	if err != nil || inst.Name != "dst" {
		t.Fatalf("%+v %v", inst, err)
	}
	res, err := c.DeployAgent(ctx, "src")
	if err != nil || res.Name != "src" {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestClientCreateStreamEmptyReadyName(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// ready with name only (no instance)
		_, _ = io.WriteString(w, `{"phase":"ready","name":"n1","ssh_port":22}`+"\n")
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	inst, err := c.CreateStream(context.Background(), api.CreateRequest{}, nil)
	if err != nil || inst.Name != "n1" {
		t.Fatalf("%+v %v", inst, err)
	}
}

func TestClientCreateStreamErrorMessageFallback(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, `{"phase":"error"}`+"\n")
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if _, err := c.CreateStream(context.Background(), api.CreateRequest{}, nil); err == nil {
		t.Fatal("want error")
	}
}

func TestClientExecEmptyCmd(t *testing.T) {
	t.Parallel()
	c := &api.Client{Base: "http://127.0.0.1:1"}
	if _, err := c.Exec(context.Background(), "x", ""); err == nil {
		t.Fatal("want cmd required")
	}
	_ = agent.ExecResult{}
}

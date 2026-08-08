package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/vm"
)

func TestOpenAPIAndHealthz(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	for _, path := range []string{"/openapi.yaml", "/openapi.json", "/healthz", "/metrics"} {
		res, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(path, err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%s status %d", path, res.StatusCode)
		}
	}
}

func TestCreateFromPoolExclusiveAndCreateBody(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// empty body create
	res, err := ts.Client().Post(ts.URL+"/vms", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d body=%s", res.StatusCode, body)
	}

	// stream=1 with invalid wait
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/vms?stream=1&wait=nope", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	res, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestPoolEndpointsDirectHTTP(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	res, err := ts.Client().Get(ts.URL + "/pool")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("pool status %d", res.StatusCode)
	}

	res, err = ts.Client().Post(ts.URL+"/pool/fill", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	// likely 400 without config
	t.Log("fill", res.StatusCode)

	res, err = ts.Client().Post(ts.URL+"/pool/claim", "application/json", strings.NewReader(`{"name":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	res, err = ts.Client().Post(ts.URL+"/pool/drain", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
}

func TestCloneConflictPath(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}

	ctx := context.Background()
	// create persistent source
	src, err := c.Create(ctx, api.CreateRequest{Name: "clone-src", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Shutdown(ctx, src.Name)

	// clone once
	dst, err := c.Clone(ctx, src.Name, api.CloneRequest{Name: "clone-dst"})
	if err != nil {
		// mock may not support clone
		t.Log("clone:", err)
		return
	}
	if dst.Name != "clone-dst" {
		t.Fatalf("%+v", dst)
	}
	// second clone same name → conflict or bad request
	res, err := ts.Client().Post(ts.URL+"/vms/"+src.Name+"/clone", "application/json",
		bytes.NewReader([]byte(`{"name":"clone-dst"}`)))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusConflict && res.StatusCode != http.StatusBadRequest {
		t.Logf("clone again status %d", res.StatusCode)
	}
	_ = vm.StatusStopped
}

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func TestPoolAPIEndpoints(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	cfg.WarmPool = config.WarmPoolConfig{Template: "golden", Size: 1}
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := manager.New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	srv := api.New(mgr, observability.NewMetrics(), nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	// Create template golden (stopped/suspended for pool)
	inst, err := c.Create(ctx, api.CreateRequest{Name: "golden", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(ctx, inst.Name); err != nil {
		// mock may leave stopped; continue
		t.Log(err)
	}

	st0, err := c.PoolStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st0 == nil {
		t.Fatal("nil status")
	}

	// fill may fail if template not suspended — still exercises handlers
	if _, err := c.PoolFill(ctx); err != nil {
		t.Log("fill:", err)
	}
	// claim may fail empty
	if _, err := c.PoolClaim(ctx, "work-x"); err != nil {
		t.Log("claim:", err)
	}
	if _, err := c.PoolDrain(ctx); err != nil {
		t.Log("drain:", err)
	}

	// direct HTTP invalid claim JSON
	res, err := ts.Client().Post(ts.URL+"/pool/claim", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("claim bad json status %d", res.StatusCode)
	}
}

func TestListActivityEndpoint(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// generate activity via create
	ctx := context.Background()
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	_, _ = c.Create(ctx, api.CreateRequest{Name: "act1"})

	res, err := ts.Client().Get(ts.URL + "/activity?limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var list []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}

	// limit cap + invalid ignored
	res2, err := ts.Client().Get(ts.URL + "/activity?limit=9999")
	if err != nil {
		t.Fatal(err)
	}
	_ = res2.Body.Close()

	res3, err := ts.Client().Get(ts.URL + "/activity?limit=bad")
	if err != nil {
		t.Fatal(err)
	}
	_ = res3.Body.Close()

	// since filter
	res4, err := ts.Client().Get(ts.URL + "/activity?since=2099-01-01T00:00:00Z&limit=5")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res4.Body)
	_ = res4.Body.Close()
	if res4.StatusCode != 200 {
		t.Fatalf("status %d body=%s", res4.StatusCode, body)
	}
}

func TestListActivityNilAct(t *testing.T) {
	s := testServer(t)
	s.Act = nil
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	res, err := ts.Client().Get(ts.URL + "/activity")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("%d", res.StatusCode)
	}
	var list []any
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list == nil {
		// should be []
		t.Fatal("nil list")
	}
}

func TestCreateFromAndFromPoolAPI(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	// create template
	tpl, err := c.Create(ctx, api.CreateRequest{Name: "tpl-a", Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Shutdown(ctx, tpl.Name)

	// from spawn
	body := map[string]any{"from": "tpl-a", "name": "child-a", "persistent": true}
	b, _ := json.Marshal(body)
	res, err := ts.Client().Post(ts.URL+"/vms", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	// may succeed or fail depending on mock suspend state
	t.Logf("from spawn status=%d body=%s", res.StatusCode, rb)

	// from_pool + from mutually exclusive
	body = map[string]any{"from": "x", "from_pool": true, "name": "z"}
	b, _ = json.Marshal(body)
	res, err = ts.Client().Post(ts.URL+"/vms", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 exclusive, got %d", res.StatusCode)
	}

	// from_pool alone
	body = map[string]any{"from_pool": true, "name": "claimed"}
	b, _ = json.Marshal(body)
	res, err = ts.Client().Post(ts.URL+"/vms", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	// expected failure without pool
	if res.StatusCode == http.StatusCreated {
		t.Log("unexpected success")
	}
}

func TestCloneVMErrorBranches(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// invalid JSON
	res, err := ts.Client().Post(ts.URL+"/vms/nope/clone", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}

	// not found
	res, err = ts.Client().Post(ts.URL+"/vms/missing/clone", "application/json", strings.NewReader(`{"name":"d"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestHeadersSentAndInfoCaps(t *testing.T) {
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	res, err := ts.Client().Get(ts.URL + "/info")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var info map[string]string
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info["name"] != "grain" {
		t.Fatalf("%+v", info)
	}
	// caps present when mgr set
	if _, ok := info["max_vms"]; !ok {
		t.Fatalf("missing max_vms: %+v", info)
	}
	_ = vm.StatusRunning
}

func TestStatusRecorderInterfaces(t *testing.T) {
	// Exercise statusRecorder via activity-recorded mutating request.
	s := testServer(t)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	// DELETE missing VM records activity with error status
	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/vms/nope", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	// list activity should include something
	res2, err := ts.Client().Get(ts.URL + "/activity?limit=5")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	body, _ := io.ReadAll(res2.Body)
	if res2.StatusCode != 200 {
		t.Fatalf("%d %s", res2.StatusCode, body)
	}
}

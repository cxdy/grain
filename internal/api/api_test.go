package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/store"
)

func testServer(t *testing.T) *api.Server {
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
	return api.New(mgr, observability.NewMetrics(), nil)
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

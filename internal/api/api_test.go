package api_test

import (
	"bufio"
	"bytes"
	"encoding/json"
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

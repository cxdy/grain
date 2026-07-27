package observability_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/observability"
)

func TestMetricsPrometheus(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics()
	m.VMsCreated.Add(2)
	m.VMsRunning.Store(1)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("code %d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	s := string(body)
	if !strings.Contains(s, "grain_vms_created_total 2") {
		t.Fatalf("body %s", s)
	}
	if !strings.Contains(s, "grain_vms_running 1") {
		t.Fatalf("body %s", s)
	}
}

func TestMetricsAllCountersInBody(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics()
	m.VMsCreated.Add(1)
	m.VMsDeleted.Add(2)
	m.VMsRunning.Store(3)
	m.CreateErrors.Add(4)
	rr := httptest.NewRecorder()
	m.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body, _ := io.ReadAll(rr.Body)
	s := string(body)
	for _, want := range []string{
		"grain_vms_created_total 1",
		"grain_vms_deleted_total 2",
		"grain_vms_running 3",
		"grain_create_errors_total 4",
		"text/plain",
	} {
		if want == "text/plain" {
			if !strings.Contains(rr.Header().Get("Content-Type"), want) {
				t.Fatalf("ct %q", rr.Header().Get("Content-Type"))
			}
			continue
		}
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

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

package observability

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Metrics is a tiny Prometheus text exposition without heavy deps.
// Optional stack scrapes GET /metrics.
type Metrics struct {
	VMsCreated   atomic.Uint64
	VMsDeleted   atomic.Uint64
	VMsRunning   atomic.Int64
	CreateErrors atomic.Uint64
}

func NewMetrics() *Metrics { return &Metrics{} }

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = fmt.Fprintf(w, "# HELP grain_vms_created_total VMs created\n")
		_, _ = fmt.Fprintf(w, "# TYPE grain_vms_created_total counter\n")
		_, _ = fmt.Fprintf(w, "grain_vms_created_total %d\n", m.VMsCreated.Load())
		_, _ = fmt.Fprintf(w, "# HELP grain_vms_deleted_total VMs deleted\n")
		_, _ = fmt.Fprintf(w, "# TYPE grain_vms_deleted_total counter\n")
		_, _ = fmt.Fprintf(w, "grain_vms_deleted_total %d\n", m.VMsDeleted.Load())
		_, _ = fmt.Fprintf(w, "# HELP grain_vms_running Current running VMs\n")
		_, _ = fmt.Fprintf(w, "# TYPE grain_vms_running gauge\n")
		_, _ = fmt.Fprintf(w, "grain_vms_running %d\n", m.VMsRunning.Load())
		_, _ = fmt.Fprintf(w, "# HELP grain_create_errors_total Failed creates\n")
		_, _ = fmt.Fprintf(w, "# TYPE grain_create_errors_total counter\n")
		_, _ = fmt.Fprintf(w, "grain_create_errors_total %d\n", m.CreateErrors.Load())
	})
}

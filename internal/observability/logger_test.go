package observability_test

import (
	"testing"

	"github.com/cxdy/grain/internal/observability"
)

func TestNewLoggerLevels(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"debug", "info", "warn", "error", "DEBUG", "WARN", "", "bogus"} {
		log := observability.NewLogger(level)
		if log == nil {
			t.Fatalf("NewLogger(%q) returned nil", level)
		}
		// Exercise the handler so level branches run fully.
		log.Info("test", "level", level)
		log.Debug("debug-msg")
		log.Warn("warn-msg")
		log.Error("error-msg")
	}
}

func TestMetricsZeroAndDeleted(t *testing.T) {
	t.Parallel()
	m := observability.NewMetrics()
	m.VMsDeleted.Add(3)
	m.CreateErrors.Add(1)
	m.VMsRunning.Store(0)

	// Handler already covered; ensure counters are readable.
	if m.VMsDeleted.Load() != 3 {
		t.Fatalf("deleted %d", m.VMsDeleted.Load())
	}
	if m.CreateErrors.Load() != 1 {
		t.Fatalf("errors %d", m.CreateErrors.Load())
	}
}

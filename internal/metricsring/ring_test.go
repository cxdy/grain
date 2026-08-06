package metricsring

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRingAppendRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.ring")
	r, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := 0; i < 10; i++ {
		if err := r.Append(Sample{
			TimeMS:   time.Now().UnixMilli() + int64(i),
			Load1:    float64(i) * 0.1,
			MemTotal: 1000,
			MemAvail: uint64(900 - i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 8 {
		t.Fatalf("len %d want 8 (capacity)", len(all))
	}
	// Oldest should be samples 2..9 (first two overwritten)
	if all[0].Load1 < 0.15 {
		t.Fatalf("oldest load %v", all[0].Load1)
	}
	// Reopen
	_ = r.Close()
	r2, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	all2, err := r2.ReadAll()
	if err != nil || len(all2) != 8 {
		t.Fatalf("%d %v", len(all2), err)
	}
}

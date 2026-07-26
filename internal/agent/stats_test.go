package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectStatsFromProc(t *testing.T) {
	dir := t.TempDir()
	// linux-style /proc fixtures
	if err := os.WriteFile(filepath.Join(dir, "uptime"), []byte("12345.67 98765.43\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mem := "MemTotal:       16384000 kB\nMemFree:         2048000 kB\nMemAvailable:     8192000 kB\n"
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte(mem), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loadavg"), []byte("0.42 0.30 0.20 1/100 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldU, oldM, oldL := procUptime, procMeminfo, procLoadavg
	procUptime = filepath.Join(dir, "uptime")
	procMeminfo = filepath.Join(dir, "meminfo")
	procLoadavg = filepath.Join(dir, "loadavg")
	t.Cleanup(func() {
		procUptime, procMeminfo, procLoadavg = oldU, oldM, oldL
	})

	st := CollectStats()
	if st.UptimeSec < 12345 || st.UptimeSec > 12346 {
		t.Fatalf("uptime %v", st.UptimeSec)
	}
	wantTotal := uint64(16384000) * 1024
	wantAvail := uint64(8192000) * 1024
	if st.MemTotal != wantTotal {
		t.Fatalf("MemTotal %d want %d", st.MemTotal, wantTotal)
	}
	if st.MemAvail != wantAvail {
		t.Fatalf("MemAvail %d want %d", st.MemAvail, wantAvail)
	}
	if st.Load1 < 0.41 || st.Load1 > 0.43 {
		t.Fatalf("Load1 %v", st.Load1)
	}
}

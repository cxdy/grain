package desktop

import (
	"context"
	"strings"
	"testing"
)

func TestRunDoctorChecks(t *testing.T) {
	cfg := Defaults()
	cfg.DataDir = t.TempDir()
	checks := RunDoctorChecks(context.Background(), cfg, nil)
	if len(checks) < 3 {
		t.Fatalf("want several checks, got %d", len(checks))
	}
	var names []string
	for _, c := range checks {
		names = append(names, c.Name)
		if c.Name == "" {
			t.Fatal("empty check name")
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "data dir") {
		t.Fatalf("missing data dir: %v", names)
	}
	if !strings.Contains(joined, "qemu-img") {
		t.Fatalf("missing qemu-img: %v", names)
	}
	pass, fail := DoctorSummary(checks)
	if pass+fail != len(checks) {
		t.Fatalf("summary %d+%d != %d", pass, fail, len(checks))
	}
}

func TestRunDoctorChecksMissingDataDir(t *testing.T) {
	cfg := Defaults()
	cfg.DataDir = "/nonexistent/grain-doctor-test-dir-xyz"
	checks := RunDoctorChecks(context.Background(), cfg, nil)
	found := false
	for _, c := range checks {
		if c.Name == "data dir" && !c.OK {
			found = true
			if c.Command == "" {
				t.Fatal("want fix command")
			}
		}
	}
	if !found {
		t.Fatal("expected data dir failure")
	}
}

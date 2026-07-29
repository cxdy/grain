package agent

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadinessMissing(t *testing.T) {
	t.Setenv("GRAIN_READINESS_DIR", t.TempDir()+"/missing")
	if got := LoadReadiness(); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestLoadReadinessAndHealth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRAIN_READINESS_DIR", dir)
	write := func(name, v string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("state", "running\n")
	write("phase", "packages")
	write("message", "installing git")
	write("ready_name", "node20-dev")
	write("updated_at", "2026-07-29T12:00:00Z")

	r := LoadReadiness()
	if r == nil {
		t.Fatal("nil readiness")
	}
	if r.State != ReadinessRunning || r.Phase != "packages" || r.Message != "installing git" {
		t.Fatalf("got %+v", r)
	}
	if r.StatusLine() == "" || r.StatusLine() != "running packages — installing git" {
		t.Fatalf("StatusLine = %q", r.StatusLine())
	}

	c := startTestServer(t)
	h, err := c.Health(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if h.Readiness == nil || h.Readiness.State != ReadinessRunning {
		t.Fatalf("health readiness: %+v", h.Readiness)
	}

	rr, err := c.Readiness(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if rr.State != ReadinessRunning {
		t.Fatalf("GET /readiness: %+v", rr)
	}
}

func TestReadinessFailedStatusLine(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GRAIN_READINESS_DIR", dir)
	_ = os.WriteFile(filepath.Join(dir, "state"), []byte(ReadinessFailed), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "error"), []byte("setup.sh exit 1"), 0o644)
	r := LoadReadiness()
	if r.StatusLine() != "failed — setup.sh exit 1" {
		t.Fatalf("StatusLine = %q", r.StatusLine())
	}
}

func TestReadinessEndpointEmpty(t *testing.T) {
	t.Setenv("GRAIN_READINESS_DIR", t.TempDir()+"/nope")
	c := startTestServer(t)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, c.BaseURL+"/readiness", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	var r Readiness
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	if r.State != "" {
		t.Fatalf("want empty state, got %+v", r)
	}
}

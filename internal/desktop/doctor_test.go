package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

func TestDoctorRepairRejectsUnknown(t *testing.T) {
	_, err := DoctorRepair(context.Background(), "rm -rf /", nil)
	if err == nil {
		t.Fatal("want reject")
	}
}

func TestImageVersionLabel(t *testing.T) {
	if imageVersionLabel("x", false) != "" {
		t.Fatal("not ready")
	}
	if imageVersionLabel("grain-ubuntu", true) != "24.04" {
		t.Fatal(imageVersionLabel("grain-ubuntu", true))
	}
	if imageVersionLabel("custom", true) != "installed" {
		t.Fatal(imageVersionLabel("custom", true))
	}
}

func TestRunDoctorChecksEmptyDataDirAndSocket(t *testing.T) {
	cfg := Defaults()
	cfg.DataDir = ""
	cfg.Socket = filepath.Join(t.TempDir(), "missing.sock")
	cfg.Image = "grain-ubuntu"
	// empty data dir branch
	checks := RunDoctorChecks(nil, cfg, nil) // nil ctx → Background
	var foundEmpty, foundSock bool
	for _, c := range checks {
		if c.Name == "data dir" && !c.OK {
			foundEmpty = true
		}
		if c.Name == "unix socket" && !c.OK {
			foundSock = true
		}
	}
	if !foundEmpty {
		t.Fatal("want empty data dir fail")
	}
	if !foundSock {
		t.Fatal("want missing socket")
	}

	// default image not ready when data dir exists but empty
	cfg2 := Defaults()
	cfg2.DataDir = t.TempDir()
	cfg2.Socket = filepath.Join(t.TempDir(), "missing.sock")
	cfg2.Image = "grain-ubuntu"
	checks2 := RunDoctorChecks(context.Background(), cfg2, nil)
	foundImage := false
	for _, c := range checks2 {
		if c.Name == "default image" && !c.OK {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatal("want default image fail")
	}
}

func TestRunDoctorChecksWithServiceHealth(t *testing.T) {
	// healthy service
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.2.3"})
	})
	sock := startFakeDaemon(t, mux)
	cfg := Defaults()
	cfg.Socket = sock
	cfg.DataDir = t.TempDir()
	cfg.Image = ""
	// present socket
	svc := NewService(cfg)
	svc.Active = "local"
	checks := RunDoctorChecks(context.Background(), cfg, svc)
	foundHealth := false
	for _, c := range checks {
		if c.Name == "daemon health" {
			foundHealth = true
			if !c.OK {
				t.Fatalf("want healthy: %+v", c)
			}
		}
	}
	if !foundHealth {
		t.Fatal("want daemon health check")
	}

	// unhealthy service (missing socket) — Health returns Healthy=false, err=nil
	cfg2 := Defaults()
	cfg2.Socket = filepath.Join(t.TempDir(), "no.sock")
	cfg2.API = "127.0.0.1:1"
	cfg2.DataDir = t.TempDir()
	svc2 := NewService(cfg2)
	checks2 := RunDoctorChecks(context.Background(), cfg2, svc2)
	foundUnhealthy := false
	for _, c := range checks2 {
		if c.Name == "daemon health" && !c.OK {
			foundUnhealthy = true
		}
	}
	if !foundUnhealthy {
		t.Fatal("want unhealthy daemon")
	}
	// Health returns error when active connection name is unknown
	svc2.Active = "does-not-exist"
	checks2b := RunDoctorChecks(context.Background(), cfg2, svc2)
	foundErr := false
	for _, c := range checks2b {
		if c.Name == "daemon health" && !c.OK {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("want health error path")
	}

	// socket present + default image ready
	dataDir := t.TempDir()
	imgDir := filepath.Join(dataDir, "images", "grain-ubuntu")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "disk.qcow2"), make([]byte, 2*1024*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg3 := Defaults()
	cfg3.DataDir = dataDir
	cfg3.Socket = sock
	cfg3.Image = "grain-ubuntu"
	checks3 := RunDoctorChecks(context.Background(), cfg3, nil)
	var sockOK, imgOK bool
	for _, c := range checks3 {
		if c.Name == "unix socket" && c.OK {
			sockOK = true
		}
		if c.Name == "default image" && c.OK {
			imgOK = true
		}
	}
	if !sockOK {
		t.Fatal("want socket ok")
	}
	if !imgOK {
		t.Fatal("want default image ready")
	}
}

func TestDoctorRepairAllowed(t *testing.T) {
	// use a fake grain binary that succeeds
	dir := t.TempDir()
	grain := filepath.Join(dir, "grain")
	// shell script that exits 0
	if err := os.WriteFile(grain, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{path: grain}
	// DoctorRepair uses exec.CommandContext with LookPath result — so path must be real executable
	res, err := DoctorRepair(context.Background(), "grain up", r)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Command != "grain up" {
		t.Fatalf("%+v", res)
	}
	// bare form without grain prefix
	res2, err := DoctorRepair(context.Background(), "up --mcp", r)
	if err != nil {
		t.Fatal(err)
	}
	if !res2.OK {
		t.Fatalf("%+v", res2)
	}
	// grain missing
	r2 := &fakeRunner{lookErr: os.ErrNotExist}
	if _, err := DoctorRepair(context.Background(), "grain up", r2); err == nil {
		t.Fatal("want grain not on PATH")
	}
	// failing command
	failBin := filepath.Join(dir, "fail")
	if err := os.WriteFile(failBin, []byte("#!/bin/sh\necho boom\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r3 := &fakeRunner{path: failBin}
	res3, err := DoctorRepair(context.Background(), "grain up", r3)
	if err == nil || res3.OK {
		t.Fatalf("want fail: %+v %v", res3, err)
	}
	// nil runner
	if _, err := DoctorRepair(context.Background(), "rm -rf /", nil); err == nil {
		t.Fatal("want reject")
	}
}

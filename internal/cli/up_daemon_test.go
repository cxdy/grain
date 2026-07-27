package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestCleanupStaleDaemonFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		DataDir: dir,
		Socket:  filepath.Join(dir, "grain.sock"),
	}
	pidPath := daemonPIDPath(cfg)
	if err := os.WriteFile(pidPath, []byte("999999991\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Socket, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupStaleDaemonFiles(cfg)
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("pid should be removed")
	}
	if _, err := os.Stat(cfg.Socket); !os.IsNotExist(err) {
		t.Fatal("socket should be removed")
	}

	// Live pid (this process) must not wipe files.
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Socket, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupStaleDaemonFiles(cfg)
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatal("live pid file must stay")
	}
}

func TestProbeDaemonHealthAndAlreadyUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")

	// remote URL is not "local daemon" for requireLocalDaemon on up — probe only.
	// Use unix-style config with HTTP via flag for Health.
	cfg := config.Config{DataDir: t.TempDir(), Socket: filepath.Join(t.TempDir(), "x.sock"), API: "127.0.0.1:1"}
	// clientFrom with apiURLFlag uses HTTP — health should pass.
	if err := probeDaemonHealth(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestCmdUpReportsFailureWhenChildDies(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	// Empty API avoids port fights; child still starts as test binary "up --fg".
	yml := fmt.Sprintf("data_dir: %s\nsocket: %s\napi: \"\"\nlog_level: error\n", dir, filepath.Join(dir, "g.sock"))
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-create a live "daemon" pid that is unhealthy (this process) so up refuses to start a second.
	if err := os.WriteFile(filepath.Join(dir, "grain.pid"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cmdUp(&cfgPath).Execute()
	if err == nil {
		t.Fatal("expected unresponsive-pid error")
	}
	if !strings.Contains(err.Error(), "not healthy") && !strings.Contains(err.Error(), "grain down") {
		t.Fatalf("got %v", err)
	}
}

func TestCmdDownStalePid(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	sock := filepath.Join(dir, "grain.sock")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("data_dir: %s\nsocket: %s\n", dir, sock)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "grain.pid"), []byte("999999991\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdDown(&cfgPath).Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "grain.pid")); !os.IsNotExist(err) {
		t.Fatal("pid cleaned")
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatal("socket cleaned")
	}
}

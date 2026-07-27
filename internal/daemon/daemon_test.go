package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/daemon"
	"github.com/cxdy/grain/internal/netutil"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunMockHypervisorCancel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pick a free loopback port for the TCP API.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiAddr := ln.Addr().String()
	_ = ln.Close()

	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "grain.sock")
	cfg.API = apiAddr
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second
	cfg.APIToken = ""

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() {
		errCh <- daemon.Run(ctx, cfg, log)
	}()

	// Wait for socket + HTTP API.
	deadline := time.Now().Add(5 * time.Second)
	var healthy bool
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.Socket); err == nil {
			res, err := http.Get("http://" + apiAddr + "/healthz")
			if err == nil {
				_ = res.Body.Close()
				if res.StatusCode == http.StatusOK {
					healthy = true
					break
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !healthy {
		cancel()
		t.Fatal("daemon did not become healthy")
	}

	// Create a VM via TCP API (mock hypervisor).
	body := `{"name":"d1","persistent":false}`
	res, err := http.Post("http://"+apiAddr+"/vms", "application/json", strings.NewReader(body))
	if err != nil {
		cancel()
		t.Fatalf("create: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		cancel()
		t.Fatalf("create status %d: %s", res.StatusCode, b)
	}
	var inst map[string]any
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		cancel()
		t.Fatal(err)
	}
	if inst["name"] != "d1" {
		cancel()
		t.Fatalf("name %v", inst["name"])
	}

	// PID file written.
	pidPath := filepath.Join(dir, "grain.pid")
	if _, err := os.Stat(pidPath); err != nil {
		cancel()
		t.Fatalf("pid file: %v", err)
	}

	// Unix socket client.
	unixClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", cfg.Socket)
			},
		},
		Timeout: 5 * time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, "http://grain/vms", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	ures, err := unixClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("unix list: %v", err)
	}
	_ = ures.Body.Close()
	if ures.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("unix list status %d", ures.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit after cancel")
	}

	// Socket and pid cleaned up.
	if _, err := os.Stat(cfg.Socket); !os.IsNotExist(err) {
		t.Fatalf("socket should be removed: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid should be removed: %v", err)
	}
}

func TestRunNonLoopbackRequiresToken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "grain.sock")
	cfg.API = "0.0.0.0:0"
	cfg.Hypervisor = "mock"
	cfg.APIToken = ""
	cfg.AuthToken = ""

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := daemon.Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("expected error for non-loopback without token")
	}
	if !strings.Contains(err.Error(), "not loopback") && !strings.Contains(err.Error(), "api_token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMockNoTCPAPI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "grain.sock")
	cfg.API = "" // unix only
	cfg.Hypervisor = "mock"
	cfg.ReadyTimeout = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.Socket); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(cfg.Socket); err != nil {
		cancel()
		t.Fatalf("socket: %v", err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit")
	}
}

func TestRunNonLoopbackWithToken(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// Bind 0.0.0.0 on an ephemeral port by first releasing a probe on loopback
	// and using a free port number (race-tolerant: listen may still fail).
	_, port, err := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "grain.sock")
	cfg.API = "0.0.0.0:" + port
	cfg.Hypervisor = "mock"
	cfg.APIToken = "daemon-tok"
	cfg.ReadyTimeout = time.Second

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- daemon.Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	deadline := time.Now().Add(5 * time.Second)
	var healthy bool
	for time.Now().Before(deadline) {
		res, err := http.Get("http://127.0.0.1:" + port + "/healthz")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				healthy = true
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !healthy {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				// Port race is acceptable; token path was attempted.
				t.Logf("daemon exit before healthy (port race ok): %v", err)
				return
			}
		default:
		}
		t.Fatal("daemon not healthy")
	}

	// Protected route without token → 401
	res, err := http.Get("http://127.0.0.1:" + port + "/info")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		cancel()
		t.Fatalf("want 401 got %d", res.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/info", nil)
	req.Header.Set("Authorization", "Bearer daemon-tok")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("want 200 with token got %d", res.StatusCode)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit")
	}
}

func TestRunAPIPortInUse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	addr := ln.Addr().String()

	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "grain.sock")
	cfg.API = addr
	cfg.Hypervisor = "mock"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = daemon.Run(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("expected listen error")
	}
	if !strings.Contains(err.Error(), "api listen") {
		t.Fatalf("error %v", err)
	}
}

func TestRunMockAndCancel(t *testing.T) {
	dir := t.TempDir()
	port, err := netutil.FreeTCPPort()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "grain.sock")
	cfg.API = fmt.Sprintf("127.0.0.1:%d", port)
	cfg.Hypervisor = "mock"
	cfg.LogLevel = "error"

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- daemon.Run(ctx, cfg, slog.Default()) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.Socket); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Run exit")
	}
}

func TestRunFirecrackerAndQEMUBranches(t *testing.T) {
	for _, hv := range []string{"firecracker", "qemu"} {
		hv := hv
		t.Run(hv, func(t *testing.T) {
			dir := t.TempDir()
			cfg := config.Defaults()
			cfg.DataDir = dir
			cfg.Socket = filepath.Join(dir, "g.sock")
			cfg.API = ""
			cfg.Hypervisor = hv
			cfg.LogLevel = "error"
			cfg.MountDriver = "9p"

			ctx, cancel := context.WithCancel(context.Background())
			errCh := make(chan error, 1)
			go func() { errCh <- daemon.Run(ctx, cfg, slog.Default()) }()
			time.Sleep(80 * time.Millisecond)
			cancel()
			select {
			case <-errCh:
			case <-time.After(5 * time.Second):
				t.Fatal("timeout")
			}
		})
	}
}

func TestRunEnsureDirsFail(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "notadir")
	if err := os.WriteFile(bad, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DataDir = bad
	cfg.Hypervisor = "mock"
	err := daemon.Run(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Fatal("expected EnsureDirs error")
	}
}

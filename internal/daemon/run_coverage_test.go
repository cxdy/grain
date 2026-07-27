package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/netutil"
)

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
	go func() { errCh <- Run(ctx, cfg, slog.Default()) }()

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
			go func() { errCh <- Run(ctx, cfg, slog.Default()) }()
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

func TestRunNonLoopbackRequiresToken(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Socket = filepath.Join(dir, "g.sock")
	cfg.API = "0.0.0.0:17999"
	cfg.APIToken = ""
	cfg.Hypervisor = "mock"
	cfg.LogLevel = "error"

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := Run(ctx, cfg, slog.Default())
	if err == nil {
		t.Fatal("expected non-loopback without token error")
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
	err := Run(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Fatal("expected EnsureDirs error")
	}
}

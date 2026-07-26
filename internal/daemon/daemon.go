package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/manager"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/store"
)

// Run starts the control plane and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	st, err := store.New(cfg.DataDir)
	if err != nil {
		return err
	}

	var rt hypervisor.Runtime
	var disk hypervisor.Disk
	switch cfg.Hypervisor {
	case "mock":
		rt = hypervisor.NewMockRuntime()
		disk = hypervisor.NewMockDisk()
	case "firecracker":
		rt = hypervisor.NewFirecrackerRuntime(cfg.FirecrackerBinary, cfg.DataDir, cfg.KernelPath)
		disk = hypervisor.NewLocalDisk(cfg.DataDir)
		log.Info("hypervisor", "backend", "firecracker", "note", "experimental; Linux only")
	default:
		qrt := hypervisor.NewQEMURuntime(cfg.QEMUBinary, cfg.DataDir)
		qrt.MountDriver = hypervisor.ResolveMountDriver(cfg.MountDriver, log)
		qrt.AgentTransport = cfg.AgentTransport
		rt = qrt
		disk = hypervisor.NewLocalDisk(cfg.DataDir)
	}

	mgr := manager.New(cfg, st, rt, disk, log)
	// ephemeral sandboxes do not survive daemon restart
	_ = mgr.CleanupEphemeral(ctx)

	met := observability.NewMetrics()
	// seed running gauge from store
	if list, err := mgr.List(); err == nil {
		var n int64
		for _, i := range list {
			if i.Status == "running" {
				n++
			}
		}
		met.VMsRunning.Store(n)
	}

	srv := api.New(mgr, met, log)
	srv.APIToken = cfg.ResolvedAPIToken()
	handler := srv.Handler()

	// TCP API (simple for curl + metrics scrape)
	var httpSrv *http.Server
	if cfg.API != "" {
		httpSrv = &http.Server{Addr: cfg.API, Handler: handler}
		go func() {
			log.Info("api listen", "addr", cfg.API)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("api server", "err", err)
			}
		}()
	}

	// Unix socket for local CLI
	_ = os.Remove(cfg.Socket)
	_ = os.MkdirAll(filepath.Dir(cfg.Socket), 0o755)
	ul, err := net.Listen("unix", cfg.Socket)
	if err != nil {
		return fmt.Errorf("socket %s: %w", cfg.Socket, err)
	}
	_ = os.Chmod(cfg.Socket, 0o600)
	unixSrv := &http.Server{Handler: handler}
	go func() {
		log.Info("socket listen", "path", cfg.Socket)
		if err := unixSrv.Serve(ul); err != nil && err != http.ErrServerClosed {
			log.Error("socket server", "err", err)
		}
	}()

	// write pid
	pidPath := filepath.Join(cfg.DataDir, "grain.pid")
	_ = os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644)

	<-ctx.Done()
	log.Info("shutting down")
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = unixSrv.Shutdown(shCtx)
	if httpSrv != nil {
		_ = httpSrv.Shutdown(shCtx)
	}
	_ = mgr.CleanupEphemeral(context.Background())
	_ = os.Remove(cfg.Socket)
	_ = os.Remove(pidPath)
	return nil
}

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

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/manager"
	grainmcp "github.com/cxdy/grain/internal/mcp"
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
		log.Info("hypervisor", "backend", "firecracker", "note", "Linux+KVM; agent + TAP publish")
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

	// Warm pool: best-effort background fill when configured (does not block listen).
	if cfg.WarmPool.Enabled() {
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			if err := mgr.EnsureWarmPool(bg); err != nil {
				log.Warn("warm pool fill on start", "err", err)
			}
		}()
	}

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

	// TCP API (simple for curl + metrics scrape + remote CLI/SDK clients)
	var httpSrv *http.Server
	if cfg.API != "" {
		// Refuse non-loopback binds without a token — open control planes are dangerous.
		if !config.ListenAddrIsLoopback(cfg.API) && cfg.ResolvedAPIToken() == "" {
			return fmt.Errorf("api listen %q is not loopback but api_token is empty — set api_token (or bind 127.0.0.1) before exposing the control plane; see https://grainvm.com/guides/remote-host/", cfg.API)
		}
		if !config.ListenAddrIsLoopback(cfg.API) {
			// Daemon serves cleartext HTTP only; non-loopback binds expose Bearer
			// tokens on the path unless operators terminate TLS or tunnel.
			log.Warn("api listen is not loopback — control plane is cleartext HTTP so Bearer tokens are sniffable on the network path; prefer 127.0.0.1 + SSH tunnel or terminate TLS with a reverse proxy; keep host firewall tight and api_token set",
				"addr", cfg.API)
		}
		// Bind before claiming success so "port in use" fails the daemon start.
		apiLn, err := net.Listen("tcp", cfg.API)
		if err != nil {
			return fmt.Errorf("api listen %s: %w", cfg.API, err)
		}
		httpSrv = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			log.Info("api listen", "addr", cfg.API, "auth", cfg.ResolvedAPIToken() != "")
			if err := httpSrv.Serve(apiLn); err != nil && err != http.ErrServerClosed {
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
	unixSrv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Info("socket listen", "path", cfg.Socket)
		if err := unixSrv.Serve(ul); err != nil && err != http.ErrServerClosed {
			log.Error("socket server", "err", err)
		}
	}()

	// Claim pid as soon as both listeners are up so a dying predecessor that
	// still has our previous PID in grain.pid cannot treat the new socket as its own.
	pidPath := filepath.Join(cfg.DataDir, "grain.pid")
	selfPID := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", selfPID)), 0o644); err != nil {
		log.Warn("write pid file", "path", pidPath, "err", err)
	}

	// Optional MCP Streamable HTTP (same process as the daemon).
	mcpCancel := func() {}
	if cfg.MCP.Enabled {
		mcpCtx, cancel := context.WithCancel(context.Background())
		mcpCancel = cancel
		go func() {
			// Dial the unix socket we just opened so tools use the live control plane.
			c, err := client.DialUnixToken(cfg.Socket, cfg.ResolvedAPIToken())
			if err != nil {
				log.Error("mcp dial local socket", "err", err)
				return
			}
			if err := grainmcp.RunHTTP(mcpCtx, cfg.MCP.Listen, api.Version, c, cfg.DataDir, log, cfg.ResolvedAPIToken()); err != nil {
				log.Error("mcp server", "err", err)
			}
		}()
	}

	<-ctx.Done()
	log.Info("shutting down", "pid", selfPID)
	mcpCancel()
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = unixSrv.Shutdown(shCtx)
	if httpSrv != nil {
		_ = httpSrv.Shutdown(shCtx)
	}
	_ = mgr.CleanupEphemeral(context.Background())
	// Never unlink the unix socket path here. A successor may have already
	// rebound grain.sock while this process was still draining Shutdown; the
	// old code deleted that new inode when grain.pid still listed this PID
	// (TOCTOU: successor listens before it overwrites the pid file), leaving
	// a live daemon on TCP with an unlinked socket. The next Start always
	// os.Remove(socket) before Listen. Only clear the pid file if we still own it.
	removePIDFileIfOwned(pidPath, selfPID, log)
	return nil
}

// removePIDFileIfOwned deletes grain.pid only when it still contains selfPID.
func removePIDFileIfOwned(pidPath string, selfPID int, log *slog.Logger) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	var filePID int
	if _, err := fmt.Sscanf(string(data), "%d", &filePID); err != nil || filePID != selfPID {
		if log != nil {
			log.Info("skip pid cleanup — file not owned by this process",
				"pid", selfPID, "file_pid", filePID)
		}
		return
	}
	_ = os.Remove(pidPath)
}

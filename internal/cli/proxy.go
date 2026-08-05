package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/proxy"
	"github.com/cxdy/grain/internal/secrets"
	"github.com/spf13/cobra"
)

func cmdProxy(cfgPath *string) *cobra.Command {
	root := &cobra.Command{
		Use:   "proxy",
		Short: "Host egress proxy (default-deny allowlist + secret injection)",
		Long: `Run a programmable HTTP(S) forward proxy for guest VMs.

Guests reach the host via SLIRP as 10.0.2.2. Default listen is 0.0.0.0:3128
so VMs can use HTTPS_PROXY=http://TOKEN@10.0.2.2:3128.

  grain proxy up [--fg] [--listen 0.0.0.0:3128]
  grain proxy down
  grain proxy allow --host api.example.com [--method POST] [--path /v1/] [--secret name]
  grain proxy deny <rule-id>
  grain proxy ls
  grain proxy client create [name]

See docs/proxy.md for the security model.`,
	}
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := loadCfg(cfgPath)
		if err != nil {
			return err
		}
		return requireLocalDaemon(cfg, "grain proxy")
	}
	root.AddCommand(
		cmdProxyUp(cfgPath),
		cmdProxyDown(cfgPath),
		cmdProxyAllow(cfgPath),
		cmdProxyDeny(cfgPath),
		cmdProxyLs(cfgPath),
		cmdProxyClient(cfgPath),
	)
	return root
}

func cmdProxyUp(cfgPath *string) *cobra.Command {
	var fg bool
	var listen string
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the egress proxy (separate process from the daemon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("listen") {
				listen = proxy.ListenFromConfig(cfg.ProxyListen)
			}
			// Fail early (including background spawn path) if non-loopback with no clients.
			if err := ensureProxyAuthForListen(cfg.DataDir, listen); err != nil {
				return err
			}
			if !fg {
				// Already running?
				if pid, err := readPID(proxy.PIDPath(cfg.DataDir)); err == nil && pidAlive(pid) {
					fmt.Printf("grain proxy already up  pid=%d  listen=%s\n", pid, listen)
					return nil
				}
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				c := exec.Command(exe, "proxy", "up", "--fg", "--listen", listen)
				if *cfgPath != "" {
					c.Args = append(c.Args, "--config", *cfgPath)
				}
				// Detach from terminal; log to file.
				logPath := filepath.Join(cfg.DataDir, "proxy", "proxy.log")
				_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
				logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				c.Stdout = logF
				c.Stderr = logF
				if err := c.Start(); err != nil {
					_ = logF.Close()
					return err
				}
				// Wait briefly for pid file.
				pidPath := proxy.PIDPath(cfg.DataDir)
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(pidPath); err == nil {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				fmt.Printf("grain proxy up  pid=%d  listen=%s\n", c.Process.Pid, listen)
				fmt.Printf("  guest:  HTTPS_PROXY=http://TOKEN@10.0.2.2:%s\n", portOf(listen))
				fmt.Printf("  log:    %s\n", logPath)
				fmt.Printf("  rules:  grain proxy ls\n")
				_ = logF.Close()
				return nil
			}
			return runProxyForeground(cfg, listen)
		},
	}
	cmd.Flags().BoolVar(&fg, "fg", false, "run in foreground")
	cmd.Flags().StringVar(&listen, "listen", proxy.DefaultListen, "listen address (0.0.0.0:3128 for SLIRP guests)")
	return cmd
}

// ensureProxyAuthForListen refuses non-loopback binds when no proxy clients exist.
// Mirrors daemon/MCP policy: open listeners on all interfaces require authentication.
func ensureProxyAuthForListen(dataDir, listen string) error {
	if config.ListenAddrIsLoopback(listen) {
		return nil
	}
	st, err := proxy.NewStore(dataDir)
	if err != nil {
		return err
	}
	clients, err := st.ListClients()
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		return fmt.Errorf("proxy listen %q is not loopback but no clients exist — create one with `grain proxy client create` before exposing the proxy", listen)
	}
	return nil
}

func runProxyForeground(cfg config.Config, listen string) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	st, err := proxy.NewStore(cfg.DataDir)
	if err != nil {
		return err
	}
	// Defense in depth if called without cmdProxyUp pre-check.
	if err := ensureProxyAuthForListen(cfg.DataDir, listen); err != nil {
		return err
	}
	sec, err := secrets.New(cfg.DataDir)
	if err != nil {
		return err
	}
	log := observability.NewLogger(cfg.LogLevel)
	if !config.ListenAddrIsLoopback(listen) {
		log.Warn("proxy listen is not loopback — ensure host firewall and client tokens; prefer 127.0.0.1 for local-only",
			"addr", listen)
	}
	srv := proxy.NewServer(st, sec, listen, log)

	pidPath := proxy.PIDPath(cfg.DataDir)
	_ = os.MkdirAll(filepath.Dir(pidPath), 0o700)
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(pidPath) }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}

func cmdProxyDown(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the egress proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			pidPath := proxy.PIDPath(cfg.DataDir)
			pid, err := readPID(pidPath)
			if err != nil {
				return fmt.Errorf("proxy not running (%v)", err)
			}
			p, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err := p.Signal(syscall.SIGTERM); err != nil {
				return err
			}
			// Wait for exit briefly.
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if !pidAlive(pid) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			_ = os.Remove(pidPath)
			fmt.Println("grain proxy down")
			return nil
		},
	}
}

func cmdProxyAllow(cfgPath *string) *cobra.Command {
	var host, method, pathPrefix, secretName string
	cmd := &cobra.Command{
		Use:   "allow",
		Short: "Add an allow rule (default-deny for everything else)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if host == "" {
				return fmt.Errorf("--host is required")
			}
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			st, err := proxy.NewStore(cfg.DataDir)
			if err != nil {
				return err
			}
			r, err := st.AddRule(proxy.Rule{
				Host:       host,
				Method:     method,
				PathPrefix: pathPrefix,
				SecretName: secretName,
			})
			if err != nil {
				return err
			}
			fmt.Printf("allow %s  host=%s", r.ID, r.Host)
			if r.Method != "" {
				fmt.Printf(" method=%s", r.Method)
			}
			if r.PathPrefix != "" {
				fmt.Printf(" path=%s", r.PathPrefix)
			}
			if r.SecretName != "" {
				fmt.Printf(" secret=%s", r.SecretName)
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "hostname or *.suffix (required)")
	cmd.Flags().StringVar(&method, "method", "", "HTTP method (empty = any; CONNECT for HTTPS tunnel)")
	cmd.Flags().StringVar(&pathPrefix, "path", "", "path prefix for plain HTTP (empty = any)")
	cmd.Flags().StringVar(&secretName, "secret", "", "host secret name to inject as Authorization: Bearer")
	return cmd
}

func cmdProxyDeny(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:     "deny <rule-id>",
		Aliases: []string{"rm"},
		Short:   "Remove an allow rule",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			st, err := proxy.NewStore(cfg.DataDir)
			if err != nil {
				return err
			}
			if err := st.RemoveRule(args[0]); err != nil {
				return err
			}
			fmt.Println("removed", args[0])
			return nil
		},
	}
}

func cmdProxyLs(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List allow rules and proxy clients",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			st, err := proxy.NewStore(cfg.DataDir)
			if err != nil {
				return err
			}
			rules, err := st.ListRules()
			if err != nil {
				return err
			}
			clients, err := st.ListClients()
			if err != nil {
				return err
			}

			// Status line
			listen := proxy.ListenFromConfig(cfg.ProxyListen)
			if pid, err := readPID(proxy.PIDPath(cfg.DataDir)); err == nil && pidAlive(pid) {
				fmt.Printf("proxy  up  pid=%d  listen=%s\n", pid, listen)
			} else {
				fmt.Printf("proxy  down  listen=%s (config)\n", listen)
			}

			if len(rules) == 0 {
				fmt.Println("rules: (none — default deny; add with: grain proxy allow --host …)")
			} else {
				fmt.Printf("%-12s %-28s %-8s %-16s %s\n", "ID", "HOST", "METHOD", "PATH", "SECRET")
				for _, r := range rules {
					method := r.Method
					if method == "" {
						method = "*"
					}
					path := r.PathPrefix
					if path == "" {
						path = "*"
					}
					sec := r.SecretName
					if sec == "" {
						sec = "-"
					}
					fmt.Printf("%-12s %-28s %-8s %-16s %s\n", r.ID, r.Host, method, path, sec)
				}
			}
			if len(clients) == 0 {
				fmt.Println("clients: (none — proxy accepts unauthenticated until you create one)")
			} else {
				fmt.Printf("clients:\n")
				fmt.Printf("  %-12s %-20s %s\n", "ID", "NAME", "TOKEN")
				for _, c := range clients {
					fmt.Printf("  %-12s %-20s %s\n", c.ID, c.Name, c.Token)
				}
			}
			return nil
		},
	}
}

func cmdProxyClient(cfgPath *string) *cobra.Command {
	root := &cobra.Command{
		Use:   "client",
		Short: "Manage proxy client tokens",
	}
	root.AddCommand(&cobra.Command{
		Use:   "create [name]",
		Short: "Create a proxy client and print its token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			st, err := proxy.NewStore(cfg.DataDir)
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			c, err := st.CreateClient(name)
			if err != nil {
				return err
			}
			listen := proxy.ListenFromConfig(cfg.ProxyListen)
			fmt.Printf("client %s  name=%s\n", c.ID, c.Name)
			fmt.Printf("token  %s\n", c.Token)
			fmt.Printf("guest  export HTTPS_PROXY=http://%s@10.0.2.2:%s\n", c.Token, portOf(listen))
			return nil
		},
	})
	return root
}

func readPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(string(b), "%d", &pid); err != nil || pid <= 0 {
		return 0, fmt.Errorf("bad pid file")
	}
	return pid, nil
}

func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, Signal(0) checks existence.
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

func portOf(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 && i < len(listen)-1 {
		p := listen[i+1:]
		if _, err := strconv.Atoi(p); err == nil {
			return p
		}
	}
	return strconv.Itoa(proxy.DefaultGuestProxyPort)
}

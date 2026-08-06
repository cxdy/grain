package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/cloudinit"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/daemon"
	"github.com/cxdy/grain/internal/guest"
	"github.com/cxdy/grain/internal/image"
	grainmcp "github.com/cxdy/grain/internal/mcp"
	"github.com/cxdy/grain/internal/observability"
	"github.com/cxdy/grain/internal/proxy"
	"github.com/cxdy/grain/internal/recipe"
	"github.com/cxdy/grain/internal/vm"
	"github.com/spf13/cobra"
)

func init() {
	// Run root + intermediate PersistentPreRun hooks (upgrade notices + local-daemon gates).
	cobra.EnableTraverseRunHooks = true
}

// Root builds the grain CLI. Short commands by design.
func Root(version string) *cobra.Command {
	api.Version = version
	var cfgPath string

	root := &cobra.Command{
		Use:   "grain",
		Short: "Fast Linux microVM sandboxes on your own hardware",
		Long: `grain runs disposable Linux VMs for tests, agents, and k3s labs.

  grain up              start daemon
  grain image pull      download base OS image
  grain image import    register local golden qcow2
  grain new             ephemeral sandbox
  grain new -p          persistent sandbox
  grain new -P 8080:80  publish host:guest ports (QEMU SLIRP hostfwd; Firecracker TAP + TCP proxy)
  grain new -v HOST:GUEST  share host dir via virtio-9p
  grain new --profile agent   named profile from config
  grain new --profile remote-coding  durable remote lab (builtin: -p, 4c/8G)
  grain new --preset docker|k3s|act   userdata presets
  grain new --recipe file.yaml  portable create + bootstrap recipe
  grain new --clone SRC       offline clone of stopped persistent VM
  grain new --arch amd64      x86_64 guest (QEMU TCG on Apple Silicon)
  grain new --gpu virtio      virtio-gpu for the guest
  grain new --network overlay share L2 between VMs
  grain new --wait agent      wait for agent (ssh|agent|userdata|bootstrap)
  grain clone SRC DST   copy stopped persistent disk as new VM (offline)
  grain recipe validate|show  check a sandbox recipe file
  grain act -- [act-args]     run GitHub Actions via act in a sandbox
  grain update [--check]      check for / install latest release
  grain mcp [--http]          MCP tool server (stdio or Streamable HTTP)
  grain up --mcp              start daemon + MCP HTTP (see config mcp:)
  grain stop / start    stop or restart a persistent VM
  grain pause / resume  QMP freeze/unfreeze guest vCPUs
  grain suspend / restore  stop process (free RAM); restore from disk/snapshot
  grain ls / rm / sh / x / cp / sync
  grain sync push|pull  host↔guest directory sync (agent required)
  grain agent health|deploy  guest agent health; redeploy over SSH (local or remote API)
  grain fs              guest readdir/stat/mkdir/rm via agent
  grain profile ls      list named + builtin profiles
  grain fwd ls/add/rm   list or live-add/remove port forwards (QEMU SSH -L; FC TCP proxy)
  grain stats [name]    guest resource stats (agent)
  grain secret ls|set|rm|inject  host secrets store
  grain proxy up|down|allow|deny|ls|client  egress allowlist proxy
  grain new --proxy     inject HTTPS_PROXY via cloud-init (SLIRP 10.0.2.2)
  grain new --publish-socket H:G  SSH streamlocal socket forward
  grain logs            guest serial / qemu logs
  grain doctor          check dependencies
  grain check-config    validate config.yaml
  grain down            stop daemon

Remote team host (CLI dials HTTP instead of local socket):

  export GRAIN_API=http://127.0.0.1:7474   # after ssh -L 7474:127.0.0.1:7474 host
  export GRAIN_TOKEN=…                     # required when API is not loopback
  grain --api http://sandbox:7474 ls       # or flag instead of env
  grain new --profile remote-coding --wait agent
  grain sync push ~/proj NAME:/work/proj
  # see https://grainvm.com/guides/remote-host/`,
		// SilenceUsage/Errors: main prints a single "error: …" line (issue #93).
		// Without SilenceErrors, cobra also prints "Error: …" and messages duplicate.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file (default ~/.grain/config.yaml)")
	root.PersistentFlags().StringVar(&apiURLFlag, "api", "", "remote daemon API URL (e.g. http://127.0.0.1:7474 via SSH -L, or https://host); overrides GRAIN_API and config api_url")

	// Soft upgrade notices (disabled via check_updates / GRAIN_CHECK_UPDATES).
	// With EnableTraverseRunHooks, this still runs for nested commands that define
	// their own PersistentPreRunE (image, proxy).
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if cmd == nil || cmd.Parent() == nil {
			return
		}
		cfg, err := loadCfg(&cfgPath)
		if err != nil {
			cfg = config.Defaults()
		}
		maybePrintUpdateNotice(cfg, version, cmd.Name())
	}

	root.AddCommand(
		cmdUp(&cfgPath),
		cmdDown(&cfgPath),
		cmdUninstall(&cfgPath),
		cmdUpdate(&cfgPath, version),
		cmdMCP(&cfgPath, version),
		cmdNew(&cfgPath),
		cmdClone(&cfgPath),
		cmdAct(&cfgPath),
		cmdRecipe(),
		cmdStop(&cfgPath),
		cmdStart(&cfgPath),
		cmdPause(&cfgPath),
		cmdResume(&cfgPath),
		cmdSuspend(&cfgPath),
		cmdRestore(&cfgPath),
		cmdPool(&cfgPath),
		cmdLs(&cfgPath),
		cmdRm(&cfgPath),
		cmdSh(&cfgPath),
		cmdX(&cfgPath),
		cmdCp(&cfgPath),
		cmdSync(&cfgPath),
		cmdFs(&cfgPath),
		cmdLogs(&cfgPath),
		cmdFwd(&cfgPath),
		cmdStats(&cfgPath),
		cmdSecret(&cfgPath),
		cmdProxy(&cfgPath),
		cmdProfile(&cfgPath),
		cmdImage(&cfgPath),
		cmdAgent(&cfgPath),
		cmdStatus(&cfgPath),
		cmdDoctor(&cfgPath),
		cmdCheckConfig(&cfgPath),
		cmdVersion(version),
	)
	return root
}

// exitCodeError carries a remote process exit code for main to os.Exit with.
type exitCodeError int

func (e exitCodeError) Error() string {
	return fmt.Sprintf("exit status %d", int(e))
}

// ExitCode implements the interface checked by cmd/grain.
func (e exitCodeError) ExitCode() int { return int(e) }

func loadCfg(path *string) (config.Config, error) {
	return config.Load(*path)
}

func cmdUp(cfgPath *string) *cobra.Command {
	fg := false
	enableMCP := false
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the grain daemon",
		Long: `Start the grain daemon (unix socket + optional TCP API).

  grain up              background daemon
  grain up --fg         foreground (logs on stderr)
  grain up --mcp        also serve MCP Streamable HTTP (see config mcp.listen)

MCP can also be enabled permanently:

  mcp:
    enabled: true
    listen: 127.0.0.1:7476
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			if enableMCP {
				cfg.MCP.Enabled = true
			}
			if err := requireLocalDaemon(cfg, "grain up"); err != nil {
				return err
			}
			log := observability.NewLogger(cfg.LogLevel)

			// Already healthy — do not spawn a second daemon.
			if err := probeDaemonHealth(cfg); err == nil {
				printDaemonUp("grain already up", cfg)
				if enableMCP {
					fmt.Fprintln(os.Stderr, "note: daemon already running — restart with grain down && grain up --mcp to attach MCP in-process, or run: grain mcp --http")
				}
				return nil
			}
			// Live pid but unhealthy (half-up / stuck on port): force user to down first.
			if pid, err := readPID(daemonPIDPath(cfg)); err == nil && pidAlive(pid) {
				return fmt.Errorf("daemon pid %d is running but not healthy — try: grain down (then grain up)", pid)
			}
			// Dead pid file and/or orphan socket from a previous crash.
			cleanupStaleDaemonFiles(cfg)

			if !fg {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				c := exec.Command(exe, "up", "--fg")
				if *cfgPath != "" {
					c.Args = append(c.Args, "--config", *cfgPath)
				}
				if cfg.MCP.Enabled {
					c.Args = append(c.Args, "--mcp")
				}
				// Detach from the controlling terminal's process group so a later
				// shell Ctrl+C does not deliver SIGINT to the daemon.
				c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
				// Persist daemon logs — background children inherit a closed TTY/pipe
				// otherwise and we lose "shutting down" / crash lines.
				logPath := filepath.Join(cfg.DataDir, "logs", "daemon.log")
				if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err == nil {
					if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
						c.Stdout = f
						c.Stderr = f
						defer func() { _ = f.Close() }()
					}
				}
				if err := c.Start(); err != nil {
					return err
				}
				child := c.Process.Pid
				// Wait until healthz works, or the child exits (bind failure, etc.).
				deadline := time.Now().Add(5 * time.Second)
				errCh := make(chan error, 1)
				go func() { errCh <- c.Wait() }()
				for time.Now().Before(deadline) {
					select {
					case waitErr := <-errCh:
						// Port busy often means a half-dead daemon is already serving TCP.
						if err := probeDaemonHealth(cfg); err == nil {
							printDaemonUp("grain already up", cfg)
							if enableMCP {
								fmt.Fprintln(os.Stderr, "note: existing daemon answered healthz (possibly via TCP); unix socket may need: grain down && grain up")
							}
							return nil
						}
						cleanupStaleDaemonFiles(cfg)
						if waitErr != nil {
							return fmt.Errorf("daemon exited during start: %w (if address already in use: lsof -nP -iTCP:7474 -sTCP:LISTEN)", waitErr)
						}
						return fmt.Errorf("daemon exited during start (check: grain up --fg)")
					default:
					}
					if err := probeDaemonHealth(cfg); err == nil {
						printDaemonUp(fmt.Sprintf("grain up  pid=%d", child), cfg)
						// Reap in background so we do not leave a zombie if the daemon is long-lived.
						// Wait already runs in errCh goroutine — process stays reaped when it exits.
						return nil
					}
					time.Sleep(50 * time.Millisecond)
				}
				// Timeout: if child still alive but not healthy, leave it and report.
				if pidAlive(child) {
					return fmt.Errorf("daemon started (pid %d) but health check timed out — try: grain up --fg", child)
				}
				select {
				case waitErr := <-errCh:
					cleanupStaleDaemonFiles(cfg)
					if waitErr != nil {
						return fmt.Errorf("daemon exited during start: %w", waitErr)
					}
				default:
					cleanupStaleDaemonFiles(cfg)
				}
				return fmt.Errorf("daemon failed to start (check: grain up --fg)")
			}
			// Ignore SIGHUP so a closed terminal/session does not stop a
			// background daemon (Setsid already detaches the process group).
			signal.Ignore(syscall.SIGHUP)
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			ctx, stop := context.WithCancel(context.Background())
			go func() {
				sig := <-sigCh
				log.Info("signal received, shutting down", "signal", sig.String(), "pid", os.Getpid())
				stop()
			}()
			defer stop()
			return daemon.Run(ctx, cfg, log)
		},
	}
	cmd.Flags().BoolVar(&fg, "fg", false, "run in foreground")
	cmd.Flags().BoolVar(&enableMCP, "mcp", false, "also serve MCP Streamable HTTP (mcp.listen, default 127.0.0.1:7476/mcp)")
	return cmd
}

func cmdDown(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the grain daemon (local only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			if err := requireLocalDaemon(cfg, "grain down"); err != nil {
				return err
			}
			pidPath := daemonPIDPath(cfg)
			pid, err := readPID(pidPath)
			if err != nil {
				// Missing pid file: only remove this config's unix socket if it is
				// not dialable (do not use TCP fallback — that can hit another daemon).
				if !sockOK(cfg.Socket) {
					_ = os.Remove(cfg.Socket)
				}
				return fmt.Errorf("daemon not running (%v)", err)
			}
			if !pidAlive(pid) {
				cleanupStaleDaemonFiles(cfg)
				fmt.Println("grain down (stale pid cleaned up)")
				return nil
			}
			p, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err := p.Signal(syscall.SIGTERM); err != nil {
				return err
			}
			// Wait briefly for exit, then SIGKILL if needed.
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				if !pidAlive(pid) {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if pidAlive(pid) {
				_ = p.Signal(syscall.SIGKILL)
			}
			cleanupStaleDaemonFiles(cfg)
			fmt.Println("grain down")
			return nil
		},
	}
}

func daemonPIDPath(cfg config.Config) string {
	return filepath.Join(cfg.DataDir, "grain.pid")
}

func probeDaemonHealth(cfg config.Config) error {
	c, err := clientFrom(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	return c.Health(ctx)
}

// cleanupStaleDaemonFiles removes pid/socket left by a dead daemon process.
// If a live pid owns the files, or this config's unix socket is still dialable,
// leave them alone. Uses unix only (not TCP fallback) so a half-dead daemon on
// :7474 cannot block cleanup of an unrelated temp socket in tests/tools.
func cleanupStaleDaemonFiles(cfg config.Config) {
	pidPath := daemonPIDPath(cfg)
	if pid, err := readPID(pidPath); err == nil && pidAlive(pid) {
		return // live process owns these files
	}
	if sockOK(cfg.Socket) {
		return
	}
	_ = os.Remove(pidPath)
	_ = os.Remove(cfg.Socket)
}

func printDaemonUp(header string, cfg config.Config) {
	fmt.Println(header)
	fmt.Printf("  socket  %s\n", cfg.Socket)
	if cfg.API != "" {
		fmt.Printf("  api     http://%s\n", cfg.API)
		fmt.Printf("  metrics http://%s/metrics\n", cfg.API)
	}
	if cfg.MCP.Enabled {
		listen := cfg.MCP.Listen
		if listen == "" {
			listen = "127.0.0.1:7476"
		}
		fmt.Printf("  mcp     %s\n", grainmcp.HTTPEndpoint(listen))
	}
}

func cmdNew(cfgPath *string) *cobra.Command {
	var persistent bool
	var name string
	var cpus, mem, disk int
	var image string
	var arch string
	var gpu string
	var network string
	var userdataFile string
	var recipePath string
	var profileName string
	var presetName string
	var publish []string
	var publishSockets []string
	var volumes []string
	var waitMode string
	var useProxy bool
	var cloneFrom string
	var fromTemplate string
	var fromPool bool
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Launch a sandbox (ephemeral by default)",
		Long: `Launch a sandbox (ephemeral by default).

  grain new -p -n lab          persistent lab
  grain new --clone lab -n lab2  offline disk clone of stopped persistent VM
  grain new --from golden -n w1  fast spawn from suspended template (loadvm)
  grain new --from-pool -n w1    claim a pre-cloned warm-pool member (see grain pool)

Clone limitations (same as grain clone): source must be stopped and persistent;
qcow2 overlays keep their backing chain; guest hostname may still match the
source; SSH/agent ports are allocated on the next start (clone is left stopped).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			// Allow boot + cloud-init (ReadyTimeout defaults to 2m; give API room).
			// Recipes with bootstrap may need longer; override when recipe sets ready_timeout.
			createTimeout := 5 * time.Minute

			if strings.TrimSpace(cloneFrom) != "" {
				return runClone(c, strings.TrimSpace(cloneFrom), name, createTimeout)
			}
			if fromPool {
				if strings.TrimSpace(fromTemplate) != "" {
					return fmt.Errorf("use either --from-pool or --from, not both")
				}
				if recipePath != "" || userdataFile != "" {
					return fmt.Errorf("use --from-pool alone (not with --recipe or --userdata-file)")
				}
				return runPoolClaim(c, name, createTimeout)
			}
			if strings.TrimSpace(fromTemplate) != "" {
				if recipePath != "" || userdataFile != "" {
					return fmt.Errorf("use --from alone (not with --recipe or --userdata-file)")
				}
				return runSpawn(c, strings.TrimSpace(fromTemplate), name, createTimeout)
			}

			var compiled *recipe.Compiled
			if recipePath != "" {
				if userdataFile != "" {
					return fmt.Errorf("use either --recipe or --userdata-file, not both")
				}
				rf, err := recipe.LoadResolved(recipe.DefaultLibraryDir(), recipePath)
				if err != nil {
					return err
				}
				compiled, err = rf.Compile()
				if err != nil {
					return err
				}
				if compiled.Timeout != "" {
					if d, err := time.ParseDuration(compiled.Timeout); err == nil && d > createTimeout {
						createTimeout = d + time.Minute
					}
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), createTimeout)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}

			var userdata string
			if compiled != nil {
				userdata = compiled.Userdata
			} else if userdataFile != "" {
				b, err := os.ReadFile(userdataFile)
				if err != nil {
					return fmt.Errorf("userdata-file: %w", err)
				}
				userdata = string(b)
			}
			if useProxy {
				proxyUD, err := buildProxyUserdata(cfg)
				if err != nil {
					return err
				}
				if strings.TrimSpace(userdata) == "" {
					userdata = proxyUD
				} else {
					userdata, err = cloudinit.MergeUserData(proxyUD, userdata)
					if err != nil {
						return fmt.Errorf("merge --proxy userdata: %w", err)
					}
				}
			}
			fwds, err := parsePublishFlags(publish)
			if err != nil {
				return err
			}
			mounts, err := parseVolumeFlags(volumes)
			if err != nil {
				return err
			}
			sockPairs, err := parsePublishSocketFlags(publishSockets)
			if err != nil {
				return err
			}
			var sockFwds []vm.SocketForward
			for _, p := range sockPairs {
				sockFwds = append(sockFwds, vm.SocketForward{HostPath: p.Host, GuestPath: p.Guest})
			}

			// Recipe fields act like a profile layer: flags > recipe > profile > defaults.
			if compiled != nil {
				if !cmd.Flags().Changed("name") && name == "" && compiled.Name != "" {
					name = compiled.Name
				}
				if !cmd.Flags().Changed("cpus") && cpus == 0 && compiled.CPUs > 0 {
					cpus = compiled.CPUs
				}
				if !cmd.Flags().Changed("mem") && mem == 0 && compiled.MemoryMB > 0 {
					mem = compiled.MemoryMB
				}
				if !cmd.Flags().Changed("disk") && disk == 0 && compiled.DiskGB > 0 {
					disk = compiled.DiskGB
				}
				if !cmd.Flags().Changed("image") && image == "" && compiled.Image != "" {
					image = compiled.Image
				}
				if !cmd.Flags().Changed("persist") && compiled.Persistent {
					persistent = true
				}
				if !cmd.Flags().Changed("arch") && arch == "" && compiled.Arch != "" {
					arch = compiled.Arch
				}
				if !cmd.Flags().Changed("gpu") && gpu == "" && compiled.GPU != "" {
					gpu = compiled.GPU
				}
				if !cmd.Flags().Changed("network") && network == "" && compiled.Network != "" {
					network = compiled.Network
				}
				if !cmd.Flags().Changed("preset") && presetName == "" && compiled.Preset != "" {
					presetName = compiled.Preset
				}
				if !cmd.Flags().Changed("wait") && waitMode == "" && compiled.Wait != "" {
					waitMode = compiled.Wait
				}
				if !cmd.Flags().Changed("publish") && len(fwds) == 0 && len(compiled.Forwards) > 0 {
					fwds = append([]vm.PortForward(nil), compiled.Forwards...)
				}
				if !cmd.Flags().Changed("volume") && len(mounts) == 0 && len(compiled.Mounts) > 0 {
					mounts = append([]vm.Mount(nil), compiled.Mounts...)
				}
				if !cmd.Flags().Changed("publish-socket") && len(sockFwds) == 0 && len(compiled.SocketForwards) > 0 {
					sockFwds = append([]vm.SocketForward(nil), compiled.SocketForwards...)
				}
			}

			// flags (explicit) > profile fields > global config defaults (daemon)
			o := config.CreateOverrides{
				CPUs:          cpus,
				CPUsSet:       cmd.Flags().Changed("cpus") || (compiled != nil && compiled.CPUs > 0),
				MemoryMB:      mem,
				MemoryMBSet:   cmd.Flags().Changed("mem") || (compiled != nil && compiled.MemoryMB > 0),
				DiskGB:        disk,
				DiskGBSet:     cmd.Flags().Changed("disk") || (compiled != nil && compiled.DiskGB > 0),
				Image:         image,
				ImageSet:      cmd.Flags().Changed("image") || (compiled != nil && compiled.Image != ""),
				Persistent:    persistent,
				PersistentSet: cmd.Flags().Changed("persist") || (compiled != nil && compiled.Persistent),
				Preset:        presetName,
				PresetSet:     cmd.Flags().Changed("preset") || (compiled != nil && compiled.Preset != ""),
				ForwardsSet:   cmd.Flags().Changed("publish") || (compiled != nil && len(compiled.Forwards) > 0),
				MountsSet:     cmd.Flags().Changed("volume") || (compiled != nil && len(compiled.Mounts) > 0),
			}
			resolved, err := cfg.ResolveCreate(profileName, o)
			if err != nil {
				return err
			}
			if !o.ForwardsSet {
				fwds = profileForwardsToVM(resolved.Forwards)
			}
			if !o.MountsSet {
				mounts = profileMountsToVM(resolved.Mounts)
			}

			// Expand userdata presets (CLI --preset, recipe.preset, or profile.preset).
			// k3s also gets default resources (when unset) and auto-forward :6443.
			cpusSet := o.CPUsSet || resolved.CPUs > 0
			memSet := o.MemoryMBSet || resolved.MemoryMB > 0
			userdata, resolved.CPUs, resolved.MemoryMB, fwds, err = applyPreset(
				resolved.Preset, userdata, resolved.CPUs, resolved.MemoryMB, cpusSet, memSet, fwds,
			)
			if err != nil {
				return err
			}

			tags := map[string]string{}
			if resolved.ProfileName != "" {
				tags["profile"] = resolved.ProfileName
			}
			if compiled != nil {
				for k, v := range compiled.Tags {
					tags[k] = v
				}
			}
			if len(tags) == 0 {
				tags = nil
			}

			req := api.CreateRequest{
				Name:           name,
				Persistent:     resolved.Persistent,
				CPUs:           resolved.CPUs,
				MemoryMB:       resolved.MemoryMB,
				DiskGB:         resolved.DiskGB,
				Image:          resolved.Image,
				Arch:           arch,
				GPU:            gpu,
				Network:        network,
				Tags:           tags,
				Userdata:       userdata,
				Forwards:       fwds,
				Mounts:         mounts,
				SocketForwards: sockFwds,
				Wait:           waitMode,
			}
			if compiled != nil && compiled.Timeout != "" {
				req.Timeout = compiled.Timeout
			}

			onEvent, stop := createProgressEvents("creating")
			start := time.Now()
			inst, err := c.CreateStream(ctx, req, onEvent)
			stop()
			if err != nil {
				return err
			}
			fmt.Printf("created %s  status=%s  image=%s  persist=%v", inst.Name, inst.Status, inst.Image, inst.Persistent)
			if inst.SSHPort > 0 {
				fmt.Printf("  ssh=:%d", inst.SSHPort)
			}
			for _, f := range inst.Forwards {
				proto := f.Proto
				if proto == "" {
					proto = "tcp"
				}
				fmt.Printf("  %s=:%d→%d", proto, f.HostPort, f.GuestPort)
			}
			for _, m := range inst.Mounts {
				fmt.Printf("  vol=%s→%s", m.Host, m.Guest)
			}
			for _, sf := range inst.SocketForwards {
				fmt.Printf("  sock=%s→%s", sf.HostPath, sf.GuestPath)
			}
			fmt.Printf("  (%s)\n", time.Since(start).Round(time.Second))
			fmt.Printf("next:  grain sh %s\n", inst.Name)
			fmt.Printf("       grain x %s -- uname -a\n", inst.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&persistent, "persist", "p", false, "keep disk after stop")
	cmd.Flags().StringVarP(&name, "name", "n", "", "vm name (default sbox-N)")
	cmd.Flags().IntVarP(&cpus, "cpus", "c", 0, "vCPUs")
	cmd.Flags().IntVarP(&mem, "mem", "m", 0, "memory MiB")
	cmd.Flags().IntVarP(&disk, "disk", "d", 0, "disk GiB")
	cmd.Flags().StringVarP(&image, "image", "i", "", "base image id (default from config)")
	cmd.Flags().StringVar(&arch, "arch", "", "guest arch: arm64|amd64 (default: host; amd64 on Apple Silicon uses QEMU TCG)")
	cmd.Flags().StringVar(&gpu, "gpu", "", "guest GPU: virtio (virtio-gpu-pci) or empty for none")
	cmd.Flags().StringVar(&network, "network", "", "slirp (default, isolated) or overlay (shared L2 between VMs)")
	cmd.Flags().StringVar(&userdataFile, "userdata-file", "", "path to cloud-init userdata or shell script")
	cmd.Flags().StringVar(&recipePath, "recipe", "", "sandbox recipe: library name (~/.grain/recipes/<name>.yaml) or file path")
	cmd.Flags().StringVar(&profileName, "profile", "", "named profile from config (flags override profile)")
	cmd.Flags().StringVar(&presetName, "preset", "", "userdata preset: docker, k3s, act (merged into cloud-init)")
	cmd.Flags().StringVar(&waitMode, "wait", "", "readiness: auto (agent if golden image), ssh, agent, userdata, or bootstrap")
	cmd.Flags().StringArrayVarP(&publish, "publish", "P", nil, "publish port HOST:GUEST or GUEST (repeatable; host 0 auto)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", nil, "share host dir HOST:GUEST via virtio-9p (repeatable; host may be . or relative)")
	cmd.Flags().StringArrayVar(&publishSockets, "publish-socket", nil, "SSH streamlocal socket forward HOSTPATH:GUESTPATH (repeatable; docker-style)")
	cmd.Flags().BoolVar(&useProxy, "proxy", false, "inject HTTP(S)_PROXY via cloud-init (guest → 10.0.2.2; requires grain proxy up)")
	cmd.Flags().StringVar(&cloneFrom, "clone", "", "offline clone of stopped persistent SRC (left stopped; use -n for destination name)")
	cmd.Flags().StringVar(&fromTemplate, "from", "", "fast create: clone stopped/suspended template and start (uses -loadvm when snapshotted; see grain suspend)")
	cmd.Flags().BoolVar(&fromPool, "from-pool", false, "claim a pre-cloned warm-pool member and start (see grain pool fill)")
	return cmd
}

// runPoolClaim claims a warm-pool member (API Create with from_pool=true).
func runPoolClaim(c *api.Client, dst string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		return fmt.Errorf("daemon not up — run: grain up (%w)", err)
	}
	start := time.Now()
	stop := createProgress("claiming from warm pool")
	inst, err := c.Create(ctx, api.CreateRequest{
		Name:       dst,
		FromPool:   true,
		Persistent: true,
	})
	stop()
	if err != nil {
		return err
	}
	fmt.Printf("claimed %s from pool  status=%s", inst.Name, inst.Status)
	if inst.SSHPort > 0 {
		fmt.Printf("  ssh=:%d", inst.SSHPort)
	}
	if inst.AgentPort > 0 {
		fmt.Printf("  agent=:%d", inst.AgentPort)
	}
	fmt.Printf("  (%s)\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// runSpawn clones a template and starts it (API Create with from=).
func runSpawn(c *api.Client, template, dst string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		return fmt.Errorf("daemon not up — run: grain up (%w)", err)
	}
	// Prefer public client package CreateRequest path via api client - check api.Client Create
	start := time.Now()
	stop := createProgress("spawning from " + template)
	// Use internal API client Create if it supports From - map to raw POST
	inst, err := c.Create(ctx, api.CreateRequest{
		Name:       dst,
		From:       template,
		Persistent: true,
	})
	stop()
	if err != nil {
		return err
	}
	fmt.Printf("spawned %s from %s  status=%s", inst.Name, template, inst.Status)
	if inst.SSHPort > 0 {
		fmt.Printf("  ssh=:%d", inst.SSHPort)
	}
	if inst.AgentPort > 0 {
		fmt.Printf("  agent=:%d", inst.AgentPort)
	}
	fmt.Printf("  (%s)\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// cmdClone copies a stopped persistent VM disk under a new name.
func cmdClone(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "clone <src> <dst>",
		Short: "Clone a stopped persistent VM (offline disk copy)",
		Long: `Copy a stopped persistent VM's root disk and metadata to a new name.

  grain stop lab
  grain clone lab lab-copy
  grain start lab-copy

Limitations (P2 offline clone):
  - Source must be persistent and not running/paused (stop first).
  - Ephemeral VMs cannot be cloned.
  - Clone is left stopped; SSH/agent and published host ports are allocated on start.
  - qcow2 overlays keep their backing chain (shared base image; small/fast copy).
  - Guest hostname/machine-id may still match the source until you reconfigure.
  - Live SSH port forwards (grain fwd add) are not copied.

API: POST /vms/{src}/clone  body: {"name":"dst"}`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			return runClone(c, args[0], args[1], 5*time.Minute)
		},
	}
}

func runClone(c *api.Client, src, dst string, timeout time.Duration) error {
	src = strings.TrimSpace(src)
	dst = strings.TrimSpace(dst)
	if src == "" {
		return fmt.Errorf("source VM name is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		return fmt.Errorf("daemon not up — run: grain up (%w)", err)
	}
	start := time.Now()
	inst, err := c.Clone(ctx, src, api.CloneRequest{Name: dst})
	if err != nil {
		return err
	}
	fmt.Printf("cloned %s → %s  status=%s  persist=%v  image=%s  (%s)\n",
		src, inst.Name, inst.Status, inst.Persistent, inst.Image, time.Since(start).Round(time.Second))
	fmt.Printf("next:  grain start %s\n", inst.Name)
	fmt.Printf("       grain sh %s\n", inst.Name)
	return nil
}

// buildProxyUserdata creates cloud-init that sets HTTP(S)_PROXY for SLIRP guests.
// Uses the first proxy client token if any; otherwise creates a "default" client.
func buildProxyUserdata(cfg config.Config) (string, error) {
	st, err := proxy.NewStore(cfg.DataDir)
	if err != nil {
		return "", err
	}
	token, err := st.FirstClientToken()
	if err != nil {
		return "", err
	}
	if token == "" {
		c, err := st.CreateClient("default")
		if err != nil {
			return "", err
		}
		token = c.Token
		_, _ = fmt.Fprintf(os.Stderr, "proxy: created client default  token=%s\n", token)
	}
	listen := proxy.ListenFromConfig(cfg.ProxyListen)
	return proxy.GuestProxyCloudConfig(token, listen), nil
}

func cmdLs(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List VMs",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			list, err := c.List(ctx)
			if err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			if len(list) == 0 {
				fmt.Println("no vms — create one:  grain new")
				return nil
			}
			fmt.Printf("%-12s %-10s %-5s %-8s %-8s %s\n", "NAME", "STATUS", "CPUS", "MEM", "SSH", "PERSIST")
			for _, i := range list {
				ssh := "-"
				if i.SSHPort > 0 {
					ssh = fmt.Sprintf(":%d", i.SSHPort)
				}
				fmt.Printf("%-12s %-10s %-5d %-8d %-8s %v\n", i.Name, i.Status, i.CPUs, i.MemoryMB, ssh, i.Persistent)
			}
			return nil
		},
	}
}

func cmdRm(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:     "rm [name]",
		Aliases: []string{"delete"},
		Short:   "Delete a VM (omit name if only one)",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := c.Delete(ctx, name); err != nil {
				return err
			}
			fmt.Println("deleted", name)
			return nil
		},
	}
}

func cmdStop(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "stop [name]",
		Short: "Stop a VM gracefully (QMP powerdown, then kill; ephemeral is deleted)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if err := c.Shutdown(ctx, name); err != nil {
				return err
			}
			fmt.Println("stopped", name)
			return nil
		},
	}
}

func cmdStart(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "start [name]",
		Short: "Start a stopped persistent VM",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			stop := createProgress("starting")
			start := time.Now()
			inst, err := c.Start(ctx, name)
			stop()
			if err != nil {
				return err
			}
			fmt.Printf("started %s  status=%s", inst.Name, inst.Status)
			if inst.SSHPort > 0 {
				fmt.Printf("  ssh=:%d", inst.SSHPort)
			}
			fmt.Printf("  (%s)\n", time.Since(start).Round(time.Second))
			return nil
		},
	}
}

func cmdPause(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "pause [name]",
		Short: "Pause a running VM (QMP stop — freezes guest vCPUs)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			if err := c.Pause(ctx, name); err != nil {
				return err
			}
			fmt.Println("paused", name)
			return nil
		},
	}
}

func cmdResume(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "resume [name]",
		Short: "Resume a paused VM (QMP cont)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			if err := c.Resume(ctx, name); err != nil {
				return err
			}
			fmt.Println("resumed", name)
			return nil
		},
	}
}

func cmdPool(cfgPath *string) *cobra.Command {
	root := &cobra.Command{
		Use:   "pool",
		Short: "Warm pool of pre-cloned suspended templates",
		Long: `Manage a warm pool of pre-cloned suspended VMs for fast claim.

Configure in ~/.grain/config.yaml:

  warm_pool:
    template: golden   # persistent suspended golden (prefer grain suspend)
    size: 2            # ready clones to keep on disk (not running)

Workflow:

  grain new -i grain-ubuntu -n golden -p --wait agent
  grain suspend golden
  # set warm_pool.template/size, restart daemon (or grain pool fill)
  grain pool fill
  grain new --from-pool -n work1   # or: grain pool claim -n work1

Pool members use disk only (suspended/stopped). Claim renames one member and
starts it with -loadvm when a suspend snapshot exists.`,
	}
	root.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show warm pool ready count and members",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadCfg(cfgPath)
				if err != nil {
					return err
				}
				c, err := clientFrom(cfg)
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := c.Health(ctx); err != nil {
					return fmt.Errorf("daemon not up — run: grain up (%w)", err)
				}
				st, err := c.PoolStatus(ctx)
				if err != nil {
					return err
				}
				en := "disabled"
				if st.Enabled {
					en = "enabled"
				}
				fmt.Printf("warm pool  %s  template=%s  ready=%d  desired=%d\n", en, st.Template, st.Ready, st.Desired)
				for _, n := range st.Members {
					fmt.Printf("  %s\n", n)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "fill",
			Short: "Clone template until ready count reaches warm_pool.size",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadCfg(cfgPath)
				if err != nil {
					return err
				}
				c, err := clientFrom(cfg)
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
				defer cancel()
				if err := c.Health(ctx); err != nil {
					return fmt.Errorf("daemon not up — run: grain up (%w)", err)
				}
				st, err := c.PoolFill(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("warm pool filled  template=%s  ready=%d  desired=%d\n", st.Template, st.Ready, st.Desired)
				return nil
			},
		},
		&cobra.Command{
			Use:   "claim",
			Short: "Claim one pool member, rename, and start (-loadvm when snapshotted)",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadCfg(cfgPath)
				if err != nil {
					return err
				}
				c, err := clientFrom(cfg)
				if err != nil {
					return err
				}
				name, _ := cmd.Flags().GetString("name")
				return runPoolClaim(c, name, 5*time.Minute)
			},
		},
		&cobra.Command{
			Use:   "drain",
			Short: "Delete all warm-pool members",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadCfg(cfgPath)
				if err != nil {
					return err
				}
				c, err := clientFrom(cfg)
				if err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				if err := c.Health(ctx); err != nil {
					return fmt.Errorf("daemon not up — run: grain up (%w)", err)
				}
				n, err := c.PoolDrain(ctx)
				if err != nil {
					return err
				}
				fmt.Printf("warm pool drained  count=%d\n", n)
				return nil
			},
		},
	)
	// claim -n
	for _, c := range root.Commands() {
		if c.Name() == "claim" {
			c.Flags().StringP("name", "n", "", "destination VM name (auto if empty)")
		}
	}
	return root
}

func cmdSuspend(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "suspend [name]",
		Short: "Suspend a persistent VM (stop QEMU, free RAM; optional qcow2 savevm)",
		Long: `Suspend stops the QEMU process and frees host RAM while keeping the disk.

Unlike pause (which freezes vCPUs with QEMU still running), suspend requires a
persistent VM. When the disk is qcow2, grain best-effort savevm's a snapshot
(grain-suspend) for fuller restore; otherwise restore cold-boots from disk.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			if err := c.Suspend(ctx, name); err != nil {
				return err
			}
			fmt.Println("suspended", name)
			return nil
		},
	}
}

func cmdRestore(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "restore [name]",
		Short: "Restore a suspended VM (loadvm snapshot when available)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			stop := createProgress("restoring")
			start := time.Now()
			inst, err := c.Restore(ctx, name)
			stop()
			if err != nil {
				return err
			}
			fmt.Printf("restored %s  status=%s", inst.Name, inst.Status)
			if inst.SSHPort > 0 {
				fmt.Printf("  ssh=:%d", inst.SSHPort)
			}
			fmt.Printf("  (%s)\n", time.Since(start).Round(time.Second))
			return nil
		},
	}
}

func getVMSSH(c *api.Client, name string) (host string, port int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inst, err := c.Get(ctx, name)
	if err != nil {
		return "", 0, err
	}
	if inst.SSHPort == 0 {
		return "", 0, fmt.Errorf("vm has no ssh port yet (need a bootable image + qemu)")
	}
	// Hostfwd is bound on the grain host loopback (not the guest IP).
	host = "127.0.0.1"
	return host, inst.SSHPort, nil
}

func grainSSHIdentity(cfg config.Config) string {
	return filepath.Join(cfg.DataDir, "ssh", "id_grain")
}

// sshBaseArgs builds quiet OpenSSH args via guest.SSHArgs (identity when present).
func sshBaseArgs(cfg config.Config, host string, port int) []string {
	id := grainSSHIdentity(cfg)
	if !fileExists(id) {
		id = ""
	}
	return guest.SSHArgs(cfg.SSHUser, host, port, id)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// resolveVMName picks a VM: explicit arg, or the only running/listed VM.
// If createIfEmpty is true and no VMs exist, creates an ephemeral one.
func resolveVMName(c *api.Client, args []string, createIfEmpty bool) (string, error) {
	if len(args) >= 1 && args[0] != "" && args[0] != "--" {
		return args[0], nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := c.List(ctx)
	if err != nil {
		return "", fmt.Errorf("daemon not up — run: grain up (%w)", err)
	}
	if len(list) == 0 {
		if !createIfEmpty {
			return "", fmt.Errorf("no vms — create one first:  grain new")
		}
		createCtx, createCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer createCancel()
		onEvent, stop := createProgressEvents("no vms — creating")
		start := time.Now()
		inst, err := c.CreateStream(createCtx, api.CreateRequest{}, onEvent)
		stop()
		if err != nil {
			return "", fmt.Errorf("auto-create failed: %w\n  try: grain image pull && grain new", err)
		}
		_, _ = fmt.Fprintf(os.Stderr, "created %s  ssh=:%d  (%s)\n", inst.Name, inst.SSHPort, time.Since(start).Round(time.Second))
		return inst.Name, nil
	}
	if len(list) == 1 {
		return list[0].Name, nil
	}
	var names []string
	for _, i := range list {
		names = append(names, i.Name)
	}
	return "", fmt.Errorf("which vm? pick one: %s\n  example: grain sh %s", strings.Join(names, ", "), names[0])
}

func cmdSh(cfgPath *string) *cobra.Command {
	var forceSSH, forceAgent bool
	cmd := &cobra.Command{
		Use:   "sh [name]",
		Short: "Shell into a VM (prefers guest agent PTY; SSH fallback; auto-creates if none)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if forceSSH && forceAgent {
				return fmt.Errorf("cannot use --ssh and --agent together")
			}
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			// Auto-create when no name given and no VMs — common first-run path.
			createIfEmpty := len(args) == 0
			name, err := resolveVMName(c, args, createIfEmpty)
			if err != nil {
				return err
			}

			// Prefer guest agent PTY when available (unless --ssh).
			// Remote CLI must use the daemon shell proxy (hostfwd is on the server).
			viaDaemon := remoteMode(cfg)
			if !forceSSH {
				err := shellViaAgent(c, name, forceAgent, viaDaemon)
				if err == nil {
					return nil
				}
				if forceAgent || viaDaemon {
					// Remote: no useful SSH fallback to hostfwd ports on the server.
					return err
				}
				// Only fall back to SSH when agent is missing/unhealthy.
				if !isAgentUnavailable(err) {
					return err
				}
			}
			if viaDaemon {
				return fmt.Errorf("remote API: interactive shell needs the guest agent (or SSH to the grain host and run grain sh there); --ssh uses hostfwd on the daemon host, not your laptop")
			}

			host, port, err := getVMSSH(c, name)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(os.Stderr, "connecting via ssh to %s …\n", name)
			ssh := exec.Command("ssh", sshBaseArgs(cfg, host, port)...)
			ssh.Stdin = os.Stdin
			ssh.Stdout = os.Stdout
			ssh.Stderr = os.Stderr
			return ssh.Run()
		},
	}
	cmd.Flags().BoolVar(&forceSSH, "ssh", false, "force SSH shell (skip guest agent)")
	cmd.Flags().BoolVar(&forceAgent, "agent", false, "force guest agent PTY only (error if unavailable)")
	return cmd
}

// shellViaAgent opens an interactive PTY. Local: dial agent hostfwd; remote: daemon /shell proxy.
func shellViaAgent(c *api.Client, name string, force bool, viaDaemon bool) error {
	if viaDaemon {
		return shellViaDaemon(c, name)
	}
	ac, err := dialGuestAgent(c, name, force)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stderr, "connecting via agent to %s …\n", name)
	// No overall timeout — interactive session lasts until the user exits.
	return ac.Shell(context.Background(), agent.ShellOpts{
		ExtraEnv: agent.HostShellExtraEnv(),
	})
}

func cmdX(cfgPath *string) *cobra.Command {
	var forceSSH, forceAgent bool
	cmd := &cobra.Command{
		Use:   "x [name] -- [cmd...]",
		Short: "Exec a command in a VM (prefers guest agent; SSH fallback)",
		// name optional: grain x -- uname -a   OR   grain x sbox-1 -- uname -a
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			if forceSSH && forceAgent {
				return fmt.Errorf("cannot use --ssh and --agent together")
			}
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}

			// Split name vs remote command. If first arg is a known VM, use it.
			var name string
			var remote []string
			if len(args) == 0 {
				return fmt.Errorf("usage: grain x [name] -- cmd args\n  example: grain new && grain x -- uname -a")
			}
			ctxList, cancelList := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancelList()
			list, err := c.List(ctxList)
			if err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			known := map[string]struct{}{}
			for _, i := range list {
				known[i.Name] = struct{}{}
			}
			if _, ok := known[args[0]]; ok {
				name = args[0]
				remote = args[1:]
			} else {
				// no explicit name — require single VM (do not auto-create for x)
				name, err = resolveVMName(c, nil, false)
				if err != nil {
					return err
				}
				remote = args
			}
			if len(remote) == 0 {
				return fmt.Errorf("missing command — example: grain x %s -- uname -a", name)
			}
			// strip leading -- if present
			if remote[0] == "--" {
				remote = remote[1:]
			}
			if len(remote) == 0 {
				return fmt.Errorf("missing command after --")
			}

			// Prefer guest agent when available (unless --ssh).
			viaDaemon := remoteMode(cfg)
			if !forceSSH {
				err := execViaAgent(c, name, remote, forceAgent, viaDaemon)
				if err == nil {
					return nil
				}
				// Remote command completed via agent (possibly non-zero) — do not SSH-fallback.
				var ec exitCodeError
				if errors.As(err, &ec) {
					return err
				}
				if forceAgent || viaDaemon {
					return err
				}
				// Only fall back to SSH when agent is missing/unhealthy.
				if !isAgentUnavailable(err) {
					return err
				}
			}
			if viaDaemon {
				return fmt.Errorf("remote API: exec needs the guest agent (or SSH to the grain host); --ssh uses hostfwd on the daemon host")
			}

			host, port, err := getVMSSH(c, name)
			if err != nil {
				return err
			}
			sshArgs := append(sshBaseArgs(cfg, host, port), "--")
			sshArgs = append(sshArgs, remote...)
			ssh := exec.Command("ssh", sshArgs...)
			ssh.Stdout = os.Stdout
			ssh.Stderr = os.Stderr
			return ssh.Run()
		},
	}
	cmd.Flags().BoolVar(&forceSSH, "ssh", false, "force SSH exec (skip guest agent)")
	cmd.Flags().BoolVar(&forceAgent, "agent", false, "force guest agent only (error if unavailable)")
	return cmd
}

func cmdAgent(cfgPath *string) *cobra.Command {
	root := &cobra.Command{
		Use:   "agent",
		Short: "Guest grain-agent helpers",
	}
	root.AddCommand(&cobra.Command{
		Use:   "health [name]",
		Short: "Check guest grain-agent health (includes readiness when present)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			h, err := c.AgentHealth(ctx, name)
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(h, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "deploy [name]",
		Short: "Install or refresh grain-agent in the guest over SSH",
		Long: `SCP the grain-agent Linux binary into the guest and enable the systemd
unit. Use after upgrading the host CLI so the guest agent picks up new
features (clipboard env, readiness fields, etc.).

Local CLI (no GRAIN_API): SCPs from this machine via SSH hostfwd.
Remote CLI (GRAIN_API): calls POST /vms/{name}/agent/deploy on the daemon so
SSH hostfwd runs on the sandbox host. The agent binary must exist on the
daemon host (just agent-linux or data_dir/agent/grain-agent-linux-$arch).

  just agent-linux          # build bin/grain-agent-linux-$arch if missing
  grain agent deploy NAME
  grain agent health NAME`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentDeploy(cfgPath, args)
		},
	})
	return root
}

func runAgentDeploy(cfgPath *string, args []string) error {
	cfg, err := loadCfg(cfgPath)
	if err != nil {
		return err
	}
	c, err := clientFrom(cfg)
	if err != nil {
		return err
	}
	name, err := resolveVMName(c, args, false)
	if err != nil {
		return err
	}

	// Remote CLI: deploy must run on the daemon host (SSH hostfwd lives there).
	if remoteMode(cfg) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		fmt.Fprintf(os.Stderr, "deploying grain-agent to %s via daemon API\n", name)
		result, err := c.DeployAgent(ctx, name)
		if err != nil {
			return err
		}
		if result.Health != nil && result.Health.AgentVersion != "" {
			fmt.Printf("deployed agent version %s on %s\n", result.Health.AgentVersion, name)
			return nil
		}
		fmt.Fprintf(os.Stderr, "deployed on %s (health not yet available; agent may still be starting)\n", name)
		return nil
	}

	// Local: SCP directly via hostfwd on this machine.
	host, port, err := getVMSSH(c, name)
	if err != nil {
		return err
	}
	bin, err := agent.LinuxBinaryPath(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("agent binary not found (run: just agent-linux): %w", err)
	}
	id := grainSSHIdentity(cfg)
	if !fileExists(id) {
		id = ""
	}
	user := cfg.SSHUser
	if user == "" {
		user = "grain"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fmt.Fprintf(os.Stderr, "deploying grain-agent to %s (ssh %s@%s:%d) from %s\n", name, user, host, port, bin)
	if err := guest.EnsureAgent(ctx, host, port, user, id, bin); err != nil {
		return err
	}
	// Best-effort health after deploy.
	hctx, hcancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer hcancel()
	h, err := c.AgentHealth(hctx, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deployed, but health check failed (agent may still be starting): %v\n", err)
		return nil
	}
	fmt.Printf("deployed agent version %s on %s\n", h.AgentVersion, name)
	return nil
}

func cmdStatus(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status [name]",
		Short: "Show VM status and guest readiness (one-liner)",
		Long: `Print a short status line for a sandbox: instance state plus guest
readiness protocol fields when the agent is reachable.

See https://grainvm.com/docs/main/explain/readiness/ for the readiness contract.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c, err := clientFrom(cfg)
			if err != nil {
				return err
			}
			name, err := resolveVMName(c, args, false)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			inst, err := c.Get(ctx, name)
			if err != nil {
				return err
			}
			line := fmt.Sprintf("%s  status=%s", inst.Name, inst.Status)
			if inst.Image != "" {
				line += "  image=" + inst.Image
			}
			h, err := c.AgentHealth(ctx, name)
			if err != nil {
				line += "  agent=unreachable"
				fmt.Println(line)
				return nil
			}
			line += "  agent=up"
			if h.UserdataRan {
				line += "  userdata=ran"
			} else {
				line += "  userdata=pending"
			}
			if h.Readiness != nil && h.Readiness.State != "" {
				line += "  readiness=" + h.Readiness.State
				if h.Readiness.Phase != "" {
					line += "  phase=" + h.Readiness.Phase
				}
				if h.Readiness.Message != "" {
					line += "  \"" + h.Readiness.Message + "\""
				} else if h.Readiness.Error != "" {
					line += "  \"" + h.Readiness.Error + "\""
				}
				if h.Readiness.ReadyName != "" {
					line += "  ready_name=" + h.Readiness.ReadyName
				}
			} else {
				line += "  readiness=none"
			}
			fmt.Println(line)
			return nil
		},
	}
}

func cmdVersion(v string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Long:  `Print the grain CLI version. Use grain update --check to compare against the latest release.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(v)
		},
	}
}

func cmdImage(cfgPath *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "image",
		Short: "Manage base images (local host data dir)",
	}
	c.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := loadCfg(cfgPath)
		if err != nil {
			return err
		}
		return requireLocalDaemon(cfg, "grain image")
	}
	c.AddCommand(
		&cobra.Command{
			Use:   "ls",
			Short: "List catalog and local images",
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadCfg(cfgPath)
				if err != nil {
					return err
				}
				return runImageLS(cfg)
			},
		},
		&cobra.Command{
			Use:   "pull [id]",
			Short: "Download a base image",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadCfg(cfgPath)
				if err != nil {
					return err
				}
				id := cfg.Image
				if len(args) == 1 {
					id = args[0]
				}
				return runImagePull(cfg, id)
			},
		},
	)

	var importID string
	imp := &cobra.Command{
		Use:   "import <path>",
		Short: "Register a local qcow2/raw disk as a catalog image",
		Long: `Copy or convert a local disk into ~/.grain/images/<id>/disk.qcow2.

Default id is grain-ubuntu (agent-ready golden image). Example:

  grain image import ./golden.qcow2
  grain image import ~/.grain/vms/bake-vm/disk.img.qcow2 --id grain-ubuntu
  grain new -i grain-ubuntu

See scripts/bake-golden.sh and docs/images.md for baking from ubuntu-cloud.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			return runImageImport(cfg, args[0], importID)
		},
	}
	imp.Flags().StringVar(&importID, "id", image.IDGrainUbuntu, "catalog image id")
	c.AddCommand(imp)
	return c
}

func cmdDoctor(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			return runDoctor(cfg)
		},
	}
}

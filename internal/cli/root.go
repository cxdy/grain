package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/daemon"
	"github.com/cxdy/grain/internal/guest"
	"github.com/cxdy/grain/internal/image"
	"github.com/cxdy/grain/internal/observability"
	"github.com/spf13/cobra"
)

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
  grain new -P 8080:80  publish host:guest ports
  grain new -v HOST:GUEST  share host dir via virtio-9p
  grain new --profile agent   named profile from config
  grain new --preset docker   userdata preset (docker|k3s)
  grain new --wait agent      wait for agent (ssh|agent|userdata)
  grain stop / start    stop or restart a persistent VM
  grain pause / resume  QMP freeze/unfreeze guest vCPUs
  grain ls / rm / sh / x / cp
  grain fs              guest readdir/stat/mkdir/rm via agent
  grain profile ls      list named profiles
  grain fwd ls/add/rm   list or live-add/remove port forwards
  grain logs            guest serial / qemu logs
  grain doctor          check dependencies
  grain down            stop daemon`,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file (default ~/.grain/config.yaml)")

	root.AddCommand(
		cmdUp(&cfgPath),
		cmdDown(&cfgPath),
		cmdNew(&cfgPath),
		cmdStop(&cfgPath),
		cmdStart(&cfgPath),
		cmdPause(&cfgPath),
		cmdResume(&cfgPath),
		cmdLs(&cfgPath),
		cmdRm(&cfgPath),
		cmdSh(&cfgPath),
		cmdX(&cfgPath),
		cmdCp(&cfgPath),
		cmdFs(&cfgPath),
		cmdLogs(&cfgPath),
		cmdFwd(&cfgPath),
		cmdProfile(&cfgPath),
		cmdImage(&cfgPath),
		cmdAgent(&cfgPath),
		cmdDoctor(&cfgPath),
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

func clientFrom(cfg config.Config) *api.Client {
	sock := cfg.Socket
	token := os.Getenv("GRAIN_TOKEN")
	if token == "" {
		token = cfg.ResolvedAPIToken()
	}
	return &api.Client{
		Base:  "http://grain",
		Token: token,
		HTTP: &http.Client{
			// No global Timeout — create waits for SSH; use request context instead.
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sock)
				},
				ResponseHeaderTimeout: 5 * time.Minute,
			},
		},
	}
}

func cmdUp(cfgPath *string) *cobra.Command {
	fg := false
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the grain daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			log := observability.NewLogger(cfg.LogLevel)
			if !fg {
				exe, err := os.Executable()
				if err != nil {
					return err
				}
				c := exec.Command(exe, "up", "--fg")
				if *cfgPath != "" {
					c.Args = append(c.Args, "--config", *cfgPath)
				}
				if err := c.Start(); err != nil {
					return err
				}
				// wait briefly for socket
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(cfg.Socket); err == nil {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				fmt.Printf("grain up  pid=%d\n", c.Process.Pid)
				fmt.Printf("  socket  %s\n", cfg.Socket)
				if cfg.API != "" {
					fmt.Printf("  api     http://%s\n", cfg.API)
					fmt.Printf("  metrics http://%s/metrics\n", cfg.API)
				}
				return nil
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return daemon.Run(ctx, cfg, log)
		},
	}
	cmd.Flags().BoolVar(&fg, "fg", false, "run in foreground")
	return cmd
}

func cmdDown(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Stop the grain daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			pidPath := filepath.Join(cfg.DataDir, "grain.pid")
			b, err := os.ReadFile(pidPath)
			if err != nil {
				return fmt.Errorf("daemon not running (%v)", err)
			}
			var pid int
			if _, err := fmt.Sscanf(string(b), "%d", &pid); err != nil || pid <= 0 {
				return fmt.Errorf("bad pid file")
			}
			p, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			if err := p.Signal(syscall.SIGTERM); err != nil {
				return err
			}
			fmt.Println("grain down")
			return nil
		},
	}
}

func cmdNew(cfgPath *string) *cobra.Command {
	var persistent bool
	var name string
	var cpus, mem, disk int
	var image string
	var userdataFile string
	var profileName string
	var presetName string
	var publish []string
	var volumes []string
	var waitMode string
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Launch a sandbox (ephemeral by default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c := clientFrom(cfg)
			// Allow boot + cloud-init (ReadyTimeout defaults to 2m; give API room).
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := c.Health(ctx); err != nil {
				return fmt.Errorf("daemon not up — run: grain up (%w)", err)
			}
			var userdata string
			if userdataFile != "" {
				b, err := os.ReadFile(userdataFile)
				if err != nil {
					return fmt.Errorf("userdata-file: %w", err)
				}
				userdata = string(b)
			}
			fwds, err := parsePublishFlags(publish)
			if err != nil {
				return err
			}
			mounts, err := parseVolumeFlags(volumes)
			if err != nil {
				return err
			}

			// flags (explicit) > profile fields > global config defaults (daemon)
			o := config.CreateOverrides{
				CPUs:          cpus,
				CPUsSet:       cmd.Flags().Changed("cpus"),
				MemoryMB:      mem,
				MemoryMBSet:   cmd.Flags().Changed("mem"),
				DiskGB:        disk,
				DiskGBSet:     cmd.Flags().Changed("disk"),
				Image:         image,
				ImageSet:      cmd.Flags().Changed("image"),
				Persistent:    persistent,
				PersistentSet: cmd.Flags().Changed("persist"),
				Preset:        presetName,
				PresetSet:     cmd.Flags().Changed("preset"),
				ForwardsSet:   cmd.Flags().Changed("publish"),
				MountsSet:     cmd.Flags().Changed("volume"),
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

			// Expand userdata presets (CLI --preset or profile.preset) before create.
			// k3s also gets default resources (when unset) and auto-forward :6443.
			cpusSet := o.CPUsSet || resolved.CPUs > 0
			memSet := o.MemoryMBSet || resolved.MemoryMB > 0
			userdata, resolved.CPUs, resolved.MemoryMB, fwds, err = applyPreset(
				resolved.Preset, userdata, resolved.CPUs, resolved.MemoryMB, cpusSet, memSet, fwds,
			)
			if err != nil {
				return err
			}

			var tags map[string]string
			if resolved.ProfileName != "" {
				tags = map[string]string{"profile": resolved.ProfileName}
			}

			onEvent, stop := createProgressEvents("creating")
			start := time.Now()
			inst, err := c.CreateStream(ctx, api.CreateRequest{
				Name:       name,
				Persistent: resolved.Persistent,
				CPUs:       resolved.CPUs,
				MemoryMB:   resolved.MemoryMB,
				DiskGB:     resolved.DiskGB,
				Image:      resolved.Image,
				Tags:       tags,
				Userdata:   userdata,
				Forwards:   fwds,
				Mounts:     mounts,
				Wait:       waitMode,
			}, onEvent)
			stop()
			if err != nil {
				return err
			}
			fmt.Printf("created %s  status=%s  persist=%v", inst.Name, inst.Status, inst.Persistent)
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
	cmd.Flags().StringVar(&userdataFile, "userdata-file", "", "path to cloud-init userdata or shell script")
	cmd.Flags().StringVar(&profileName, "profile", "", "named profile from config (flags override profile)")
	cmd.Flags().StringVar(&presetName, "preset", "", "userdata preset: docker, k3s (merged into cloud-init)")
	cmd.Flags().StringVar(&waitMode, "wait", "ssh", "readiness: ssh (default), agent, or userdata")
	cmd.Flags().StringArrayVarP(&publish, "publish", "P", nil, "publish port HOST:GUEST or GUEST (repeatable; host 0 auto)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", nil, "share host dir HOST:GUEST via virtio-9p (repeatable; host may be . or relative)")
	return cmd
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
			c := clientFrom(cfg)
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
			c := clientFrom(cfg)
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
			c := clientFrom(cfg)
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
			c := clientFrom(cfg)
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
			c := clientFrom(cfg)
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
			c := clientFrom(cfg)
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

func getVMSSH(c *api.Client, name string) (host string, port int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/vms/"+name, nil)
	if err != nil {
		return "", 0, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer res.Body.Close()
	var inst struct {
		SSHPort int    `json:"ssh_port"`
		IP      string `json:"ip"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return "", 0, err
	}
	if res.StatusCode >= 300 {
		if inst.Error != "" {
			return "", 0, fmt.Errorf("%s", inst.Error)
		}
		return "", 0, fmt.Errorf("status %d", res.StatusCode)
	}
	if inst.SSHPort == 0 {
		return "", 0, fmt.Errorf("vm has no ssh port yet (need a bootable image + qemu)")
	}
	host = inst.IP
	if host == "" {
		host = "127.0.0.1"
	}
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
		fmt.Fprintf(os.Stderr, "created %s  ssh=:%d  (%s)\n", inst.Name, inst.SSHPort, time.Since(start).Round(time.Second))
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
	return &cobra.Command{
		Use:   "sh [name]",
		Short: "Shell into a VM (auto-creates one if none exist)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c := clientFrom(cfg)
			// Auto-create when no name given and no VMs — common first-run path.
			createIfEmpty := len(args) == 0
			name, err := resolveVMName(c, args, createIfEmpty)
			if err != nil {
				return err
			}
			host, port, err := getVMSSH(c, name)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "connecting to %s …\n", name)
			ssh := exec.Command("ssh", sshBaseArgs(cfg, host, port)...)
			ssh.Stdin = os.Stdin
			ssh.Stdout = os.Stdout
			ssh.Stderr = os.Stderr
			return ssh.Run()
		},
	}
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
			c := clientFrom(cfg)

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
			if !forceSSH {
				err := execViaAgent(c, name, remote, forceAgent)
				if err == nil {
					return nil
				}
				// Remote command completed via agent (possibly non-zero) — do not SSH-fallback.
				var ec exitCodeError
				if errors.As(err, &ec) {
					return err
				}
				if forceAgent {
					return err
				}
				// Only fall back to SSH when agent is missing/unhealthy.
				if !isAgentUnavailable(err) {
					return err
				}
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
		Short: "Check guest grain-agent health",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c := clientFrom(cfg)
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
	return root
}

func cmdVersion(v string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(v)
		},
	}
}

func cmdImage(cfgPath *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "image",
		Short: "Manage base images",
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

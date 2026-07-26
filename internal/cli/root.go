package cli

import (
	"context"
	"encoding/json"
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
  grain new             ephemeral sandbox
  grain new -p          persistent sandbox
  grain new -P 8080:80  publish host:guest ports
  grain stop / start    stop or restart a persistent VM
  grain ls / rm / sh / x / cp
  grain fwd ls          list port forwards
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
		cmdLs(&cfgPath),
		cmdRm(&cfgPath),
		cmdSh(&cfgPath),
		cmdX(&cfgPath),
		cmdCp(&cfgPath),
		cmdLogs(&cfgPath),
		cmdFwd(&cfgPath),
		cmdImage(&cfgPath),
		cmdDoctor(&cfgPath),
		cmdVersion(version),
	)
	return root
}

func loadCfg(path *string) (config.Config, error) {
	return config.Load(*path)
}

func clientFrom(cfg config.Config) *api.Client {
	sock := cfg.Socket
	return &api.Client{
		Base: "http://grain",
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
	var publish []string
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
			onEvent, stop := createProgressEvents("creating")
			start := time.Now()
			inst, err := c.CreateStream(ctx, api.CreateRequest{
				Name:       name,
				Persistent: persistent,
				CPUs:       cpus,
				MemoryMB:   mem,
				DiskGB:     disk,
				Image:      image,
				Userdata:   userdata,
				Forwards:   fwds,
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
	cmd.Flags().StringArrayVarP(&publish, "publish", "P", nil, "publish port HOST:GUEST or GUEST (repeatable; host 0 auto)")
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
		Short: "Stop a VM (ephemeral is deleted; persistent stays on disk)",
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
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	return &cobra.Command{
		Use:   "x [name] -- [cmd...]",
		Short: "Exec a command in a VM (name optional if only one)",
		// name optional: grain x -- uname -a   OR   grain x sbox-1 -- uname -a
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			// If first token looks like a flag remnant, error
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			list, err := c.List(ctx)
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
}

func cmdCp(cfgPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "cp [src] [dst]",
		Short: "Copy files (host path or NAME:path)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg(cfgPath)
			if err != nil {
				return err
			}
			c := clientFrom(cfg)

			resolve := func(s string) (spec string, port int, err error) {
				// NAME:path → user@host:path
				if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
					name, path := s[:i], s[i+1:]
					host, p, err := getVMSSH(c, name)
					if err != nil {
						return "", 0, err
					}
					return fmt.Sprintf("%s@%s:%s", cfg.SSHUser, host, path), p, nil
				}
				return s, 0, nil
			}

			src, p1, err := resolve(args[0])
			if err != nil {
				return err
			}
			dst, p2, err := resolve(args[1])
			if err != nil {
				return err
			}
			port := p1
			if p2 > 0 {
				port = p2
			}
			id := grainSSHIdentity(cfg)
			if !fileExists(id) {
				id = ""
			}
			scpArgs := guest.SCPArgs(port, id)
			scpArgs = append(scpArgs, src, dst)
			scp := exec.Command("scp", scpArgs...)
			scp.Stdout = os.Stdout
			scp.Stderr = os.Stderr
			return scp.Run()
		},
	}
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

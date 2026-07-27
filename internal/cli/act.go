package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/presets"
	"github.com/cxdy/grain/internal/vm"
	"github.com/spf13/cobra"
)

// cmdAct runs nektos/act inside an ephemeral grain sandbox.
//
//	grain act -- -l
//	grain act -- -j test
//	grain act --keep -- -W .github/workflows/ci.yml
func cmdAct(cfgPath *string) *cobra.Command {
	var (
		keep    bool
		dir     string
		name    string
		cpus    int
		mem     int
		image   string
		timeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "act [-- grain-flags] -- [act-args...]",
		Short: "Run GitHub Actions via act inside a grain sandbox",
		Long: `Run nektos/act inside an ephemeral Linux microVM.

Creates a sandbox with the "act" preset (Docker Engine + act), mounts your
project at /work, waits until docker and act are ready, then runs act.

Pass act arguments after -- so grain does not parse them:

  grain act -- -l
  grain act -- -j test
  grain act -- -W .github/workflows/ci.yml
  grain act --keep -- -j build

Requires: grain daemon (grain up), QEMU, network (to install act on first boot
unless using a pre-baked image). The sandbox is deleted when act exits unless
--keep is set.

Equivalent manual flow:

  grain new --preset act -v "$PWD:/work" --wait agent
  grain x -- bash -lc 'cd /work && act -l'
  grain rm`,
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGrainAct(cfgPath, actOpts{
				Keep:    keep,
				Dir:     dir,
				Name:    name,
				CPUs:    cpus,
				Mem:     mem,
				Image:   image,
				Timeout: timeout,
				ActArgs: stripLeadingDashDash(args),
			})
		},
	}
	cmd.Flags().BoolVar(&keep, "keep", false, "keep the sandbox after act exits")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory to mount at /work (default: current directory)")
	cmd.Flags().StringVar(&name, "name", "", "sandbox name (default: act-<dirname>)")
	cmd.Flags().IntVar(&cpus, "cpus", 0, "vCPUs (default 2)")
	cmd.Flags().IntVar(&mem, "mem", 0, "memory MiB (default 4096)")
	cmd.Flags().StringVar(&image, "image", "", "base image id (default: auto)")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Minute, "overall timeout for create + act")
	return cmd
}

type actOpts struct {
	Keep    bool
	Dir     string
	Name    string
	CPUs    int
	Mem     int
	Image   string
	Timeout time.Duration
	ActArgs []string
}

func runGrainAct(cfgPath *string, o actOpts) error {
	cfg, err := loadCfg(cfgPath)
	if err != nil {
		return err
	}
	c, err := clientFrom(cfg)
	if err != nil {
		return err
	}

	hctx, hcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer hcancel()
	if err := c.Health(hctx); err != nil {
		return fmt.Errorf("daemon not up — run: grain up (%w)", err)
	}

	workDir := o.Dir
	if workDir == "" {
		workDir, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return err
	}
	st, err := os.Stat(workDir)
	if err != nil {
		return fmt.Errorf("project dir: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("project dir is not a directory: %s", workDir)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".github", "workflows")); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no .github/workflows under %s\n", workDir)
	}

	actArgs := o.ActArgs
	if len(actArgs) == 0 {
		actArgs = []string{"-l"}
	}

	cpus, mem := o.CPUs, o.Mem
	dc, dm := presets.DefaultResources("act")
	if cpus <= 0 {
		if dc > 0 {
			cpus = dc
		} else {
			cpus = 2
		}
	}
	if mem <= 0 {
		if dm > 0 {
			mem = dm
		} else {
			mem = 4096
		}
	}

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}

	vmName := o.Name
	if vmName == "" {
		vmName = sanitizeActName("act-" + filepath.Base(workDir))
	}

	userdata, err := presets.Get("act")
	if err != nil {
		return err
	}

	// WaitAgent deploys grain-agent over SSH when the base image is not golden.
	// Fail fast if neither a deploy binary nor a golden image is available.
	if _, err := agent.LinuxBinaryPath(cfg.DataDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: no grain-agent linux binary — WaitAgent will need a golden image or: make agent-linux\n  (%v)\n", err)
	}

	fmt.Fprintf(os.Stderr, "grain act  project=%s  vm=%s  cpus=%d mem=%d\n", workDir, vmName, cpus, mem)

	createCtx, createCancel := context.WithTimeout(context.Background(), timeout)
	defer createCancel()

	onEvent, stopProg := createProgressEvents("act sandbox")
	inst, err := c.CreateStream(createCtx, api.CreateRequest{
		Name:       vmName,
		Persistent: false,
		CPUs:       cpus,
		MemoryMB:   mem,
		Image:      o.Image,
		Userdata:   userdata,
		Mounts: []vm.Mount{
			{Host: workDir, Guest: "/work"},
		},
		// agent: short probe, then SSH-deploy grain-agent (requires make agent-linux
		// or a golden grain-ubuntu image). Docker/act packages install via cloud-init.
		Wait:    "agent",
		Timeout: timeout.String(),
		Tags:    map[string]string{"preset": "act", "grain": "act"},
	}, onEvent)
	stopProg()
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	vmName = inst.Name

	if !o.Keep {
		defer func() {
			dctx, dcancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer dcancel()
			fmt.Fprintf(os.Stderr, "grain act  deleting %s\n", vmName)
			if err := c.Delete(dctx, vmName); err != nil {
				fmt.Fprintf(os.Stderr, "grain act  warning: delete %s: %v\n", vmName, err)
			}
		}()
	} else {
		fmt.Fprintf(os.Stderr, "grain act  keeping %s — grain sh %s\n", vmName, vmName)
	}

	readyDeadline := time.Now().Add(timeout)
	if err := waitActReady(c, vmName, readyDeadline); err != nil {
		return err
	}

	// Guest agent exec has a minimal env (no login shell). act requires HOME;
	// docker/act may live under /usr/local/bin.
	shellCmd := "export HOME=\"${HOME:-/home/ubuntu}\" PATH=\"/usr/local/bin:/usr/bin:/bin:$PATH\"; cd /work && act " + shellJoin(actArgs)
	fmt.Fprintf(os.Stderr, "grain act  exec: %s\n", shellCmd)

	execCtx, execCancel := context.WithTimeout(context.Background(), time.Until(readyDeadline))
	if time.Until(readyDeadline) < time.Minute {
		execCancel()
		execCtx, execCancel = context.WithTimeout(context.Background(), timeout)
	}
	defer execCancel()

	code, err := c.ExecStream(execCtx, vmName, agent.ExecOpts{
		Cmd:  "bash",
		Args: []string{"-lc", shellCmd},
	}, func(f agent.ExecFrame) error {
		switch f.Type {
		case "stdout":
			fmt.Print(f.Data)
		case "stderr":
			fmt.Fprint(os.Stderr, f.Data)
		}
		return nil
	})
	if err != nil {
		// Fall back to buffered exec via agent dial
		if err2 := execViaAgent(c, vmName, []string{"bash", "-lc", shellCmd}, true, true); err2 != nil {
			return fmt.Errorf("act: %w (stream: %v)", err2, err)
		}
		return nil
	}
	if code != 0 {
		return exitCodeError(code)
	}
	return nil
}

// waitActReady polls until docker and act are available in the guest.
func waitActReady(c *api.Client, name string, deadline time.Time) error {
	fmt.Fprintln(os.Stderr, "grain act  waiting for docker + act …")
	probe := `bash -lc 'command -v act >/dev/null && docker info >/dev/null 2>&1'`
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		res, err := c.Exec(ctx, name, "bash", "-lc", "command -v act >/dev/null && docker info >/dev/null 2>&1 && echo READY")
		cancel()
		if err == nil && res != nil && strings.Contains(res.Stdout, "READY") {
			fmt.Fprintln(os.Stderr, "grain act  ready")
			return nil
		}
		// soft progress
		time.Sleep(3 * time.Second)
	}
	_ = probe
	return fmt.Errorf("timeout waiting for docker and act in %s (try: grain logs %s)", name, name)
}

func stripLeadingDashDash(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

// shellJoin quotes args for embedding in bash -lc.
func shellJoin(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// Safe unquoted: alnum and a few chars common in act flags
	safe := true
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(".-_/=@+,:", r) {
			continue
		}
		safe = false
		break
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// grain Valid: ^[a-z][a-z0-9-]{0,62}$
var actNameBad = regexp.MustCompile(`[^a-z0-9-]+`)

// sanitizeActName makes a valid grain VM name from a directory base.
func sanitizeActName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = actNameBad.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "act"
	}
	if len(s) > 48 {
		s = s[:48]
		s = strings.Trim(s, "-")
	}
	if s == "" {
		s = "act"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "act-" + s
	}
	return s
}

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hostbin"
	"github.com/cxdy/grain/internal/image"
	"github.com/cxdy/grain/internal/sshkey"
)

func runImageLS(cfg config.Config) error {
	cat := image.Catalog()
	imgs := image.NewManager(cfg.DataDir)
	fmt.Printf("%-16s %-8s %-8s %s\n", "ID", "LOCAL", "AGENT", "DESCRIPTION")
	for id, spec := range cat {
		local := "no"
		if imgs.Ready(id) {
			local = "yes"
		}
		agent := "no"
		if imgs.ImageHasAgent(id) || spec.HasAgent {
			agent = "yes"
		}
		desc := spec.Description
		if spec.LocalOnly {
			desc += " (import)"
		} else if spec.URL == "" {
			desc += " (unavailable on " + runtime.GOARCH + ")"
		}
		fmt.Printf("%-16s %-8s %-8s %s\n", id, local, agent, desc)
	}
	return nil
}

func runImagePull(cfg config.Config, id string) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	m := image.NewManager(cfg.DataDir)
	spec, err := image.Get(id)
	if err != nil {
		return err
	}
	if spec.LocalOnly {
		return fmt.Errorf("image %q is local-only — run: grain image import <path> --id %s", id, id)
	}
	if spec.URL == "" {
		return fmt.Errorf("image %q cannot be pulled on %s", id, runtime.GOARCH)
	}
	fmt.Printf("pulling %s …\n", id)
	start := time.Now()
	var last int64
	err = m.Pull(context.Background(), id, func(written, total int64) {
		if written-last < 5*1024*1024 && written != total {
			return
		}
		last = written
		if total > 0 {
			fmt.Printf("\r  %d / %d MB", written/1024/1024, total/1024/1024)
		} else {
			fmt.Printf("\r  %d MB", written/1024/1024)
		}
	})
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Printf("ok %s in %s\n", id, time.Since(start).Round(time.Second))
	// sync default ssh user hint
	if spec.SSHUser != "" {
		fmt.Printf("ssh user: %s\n", spec.SSHUser)
	}
	return nil
}

func runImageImport(cfg config.Config, srcPath, id string) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	if id == "" {
		id = image.IDGrainUbuntu
	}
	if _, err := image.Get(id); err != nil {
		return err
	}
	m := image.NewManager(cfg.DataDir)
	fmt.Printf("importing %s → %s …\n", srcPath, id)
	start := time.Now()
	if err := m.Import(context.Background(), id, srcPath); err != nil {
		return err
	}
	p, err := m.DiskPath(id)
	if err != nil {
		return err
	}
	fmt.Printf("ok %s in %s\n", id, time.Since(start).Round(time.Second))
	fmt.Printf("disk: %s\n", p)
	if m.ImageHasAgent(id) {
		fmt.Println("has_agent: true (create will prefer guest agent wait)")
	}
	spec, _ := image.Get(id)
	if spec.SSHUser != "" {
		fmt.Printf("ssh user: %s\n", spec.SSHUser)
	}
	fmt.Printf("use: grain new -i %s\n", id)
	return nil
}

func runDoctor(cfg config.Config) error {
	fmt.Println("grain doctor")
	ok := true
	check := func(name string, fn func() error) {
		if err := fn(); err != nil {
			fmt.Printf("  ✗ %s: %v\n", name, err)
			ok = false
			return
		}
		fmt.Printf("  ✓ %s\n", name)
	}

	check("data dir", func() error {
		return cfg.EnsureDirs()
	})
	check("ssh key", func() error {
		_, _, err := sshkey.Ensure(cfg.DataDir)
		return err
	})

	hv := strings.ToLower(strings.TrimSpace(cfg.Hypervisor))
	if hv == "" {
		hv = "qemu"
	}

	// Firecracker backend (experimental, Linux only).
	if hv == "firecracker" {
		fcBin := cfg.FirecrackerBinary
		if fcBin == "" {
			fcBin = "firecracker"
		}
		check(fcBin, func() error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("firecracker requires linux (current OS: %s)", runtime.GOOS)
			}
			_, err := exec.LookPath(fcBin)
			if err != nil {
				return fmt.Errorf("not found — install firecracker or set firecracker_binary")
			}
			return nil
		})
		// KVM is mandatory for Firecracker (no TCG fallback, unlike QEMU).
		// Skip the node check on non-Linux — the OS gate above already fails doctor.
		if runtime.GOOS == "linux" {
			check("/dev/kvm", checkDevKVM)
			if hint := kvmNestedVirtHint(); hint != "" {
				fmt.Printf("  · %s\n", hint)
			}
		}
		// Kernel soft check (Start will hard-fail if missing).
		kpath := strings.TrimSpace(cfg.KernelPath)
		if kpath == "" {
			kpath = filepath.Join(cfg.DataDir, "kernels", "vmlinux")
		}
		if st, err := os.Stat(kpath); err == nil && st.Size() > 0 {
			fmt.Printf("  ✓ firecracker kernel %s\n", kpath)
		} else {
			fmt.Printf("  · firecracker kernel missing — set kernel_path or place vmlinux at %s\n", kpath)
		}
	}

	// QEMU binary: required for default hypervisor; soft note when using firecracker/mock.
	qemu := cfg.QEMUBinary
	if qemu == "" {
		if runtime.GOARCH == "arm64" {
			qemu = "qemu-system-aarch64"
		} else {
			qemu = "qemu-system-x86_64"
		}
	}
	if hv == "qemu" || hv == "" {
		if p, err := hostbin.LookPath(qemu); err != nil {
			fmt.Printf("  ✗ %s: not found (brew install qemu)\n", qemu)
			ok = false
		} else if off, found := hostbin.FoundOffPATH(filepath.Base(qemu)); found {
			fmt.Printf("  ✓ %s (%s — not on PATH; grain still finds it)\n", qemu, off)
			_ = p
		} else {
			fmt.Printf("  ✓ %s\n", qemu)
		}
	} else if p, err := hostbin.LookPath(qemu); err == nil {
		fmt.Printf("  ✓ %s (optional; hypervisor=%s)\n", p, hv)
	} else {
		fmt.Printf("  · %s not found (ok for hypervisor=%s)\n", qemu, hv)
	}
	if p, err := hostbin.LookPath("qemu-img"); err != nil {
		if hv == "firecracker" {
			fmt.Printf("  ✗ qemu-img: not found — needed to convert qcow2 rootfs to raw for firecracker\n")
			ok = false
		} else {
			fmt.Printf("  ✗ qemu-img: not found (brew install qemu)\n")
			ok = false
		}
	} else if off, found := hostbin.FoundOffPATH("qemu-img"); found {
		fmt.Printf("  ✓ qemu-img (%s — not on PATH; grain still finds it)\n", off)
		_ = p
	} else {
		fmt.Printf("  ✓ qemu-img\n")
	}
	if runtime.GOOS == "darwin" {
		check("hdiutil (cloud-init seed)", func() error {
			_, err := exec.LookPath("hdiutil")
			return err
		})
	}

	imgs := image.NewManager(cfg.DataDir)
	def := cfg.Image
	if def == "" || def == "auto" {
		def = image.DefaultIDFor(cfg.DataDir)
	}
	check("base image "+def, func() error {
		if !imgs.Ready(def) {
			return fmt.Errorf("missing — run: grain image pull %s", def)
		}
		return nil
	})

	// Guest agent binary (soft): needed for SSH deploy into non-golden images.
	if path, err := agent.LinuxBinaryPath(cfg.DataDir); err == nil {
		fmt.Printf("  ✓ guest agent binary %s\n", path)
	} else {
		fmt.Printf("  · guest agent binary missing — run: just agent-linux (SSH-only VMs still work)\n")
	}

	// QMP capability (optional soft check): pause/resume/graceful stop use QMP.
	if hv == "qemu" || hv == "" {
		if p, err := hostbin.LookPath(qemu); err == nil {
			if qemuSupportsQMP(p) {
				fmt.Printf("  ✓ qmp (%s -qmp)\n", p)
			} else {
				fmt.Printf("  · qmp: could not confirm -qmp on %s (pause/resume may be unavailable)\n", p)
			}
		}
	}

	// socket
	if _, err := os.Stat(cfg.Socket); err == nil {
		fmt.Printf("  ✓ daemon socket %s\n", cfg.Socket)
	} else {
		fmt.Printf("  · daemon not running (grain up)\n")
	}

	if !ok {
		return fmt.Errorf("doctor found issues")
	}
	fmt.Println("all good")
	return nil
}

// kvmDevicePath is the character device Firecracker needs. Overridable in tests.
var kvmDevicePath = "/dev/kvm"

// checkDevKVM reports whether /dev/kvm exists and is usable (RDWR open).
func checkDevKVM() error {
	path := kvmDevicePath
	if path == "" {
		path = "/dev/kvm"
	}
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("missing — Firecracker requires KVM (%s). If this host is itself a VM, enable nested virtualization on the outer hypervisor so the guest CPU exposes vmx (Intel) or svm (AMD), then modprobe kvm && modprobe kvm_intel|kvm_amd", path)
	}
	if st.IsDir() {
		return fmt.Errorf("%s is a directory (expected char device)", path)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("not accessible (%v) — add the grain daemon user to the kvm group or grant RW on %s (e.g. setfacl -m u:$(whoami):rw %s)", err, path, path)
	}
	_ = f.Close()
	return nil
}

// cpuinfoPath is read by kvmNestedVirtHint. Overridable in tests.
var cpuinfoPath = "/proc/cpuinfo"

// kvmNestedVirtHint returns a soft advisory when CPU flags look incompatible
// with nested KVM (empty string = no note). Linux-only; reads /proc/cpuinfo.
func kvmNestedVirtHint() string {
	path := cpuinfoPath
	if path == "" {
		path = "/proc/cpuinfo"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// flags lines only — avoid matching model names.
	hasHV := false
	hasNest := false
	for _, line := range strings.Split(string(b), "\n") {
		low := strings.ToLower(line)
		if !strings.HasPrefix(low, "flags") && !strings.HasPrefix(low, "features") {
			continue
		}
		// Token match so we do not hit substrings inside other flags.
		fields := strings.Fields(low)
		for _, f := range fields {
			switch f {
			case "hypervisor":
				hasHV = true
			case "vmx", "svm":
				hasNest = true
			}
		}
	}
	if hasHV && !hasNest {
		return "CPU is a VM without nested virt flags (no vmx/svm) — enable nested virtualization on the outer host or Firecracker cannot use KVM"
	}
	if !hasNest {
		return "CPU flags lack vmx/svm — hardware virtualization may be disabled in firmware/BIOS"
	}
	return ""
}

// qemuSupportsQMP reports whether the QEMU binary documents a -qmp flag.
// Soft check only — does not dial a live monitor socket.
func qemuSupportsQMP(qemuBin string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, qemuBin, "-help")
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		// Some builds use -h; try once more.
		cmd = exec.CommandContext(ctx, qemuBin, "-h")
		out, err = cmd.CombinedOutput()
		if err != nil && len(out) == 0 {
			return false
		}
	}
	s := strings.ToLower(string(out))
	return strings.Contains(s, "-qmp") || strings.Contains(s, "qmp")
}

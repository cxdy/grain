package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/cxdy/grain/internal/config"
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

	qemu := cfg.QEMUBinary
	if qemu == "" {
		if runtime.GOARCH == "arm64" {
			qemu = "qemu-system-aarch64"
		} else {
			qemu = "qemu-system-x86_64"
		}
	}
	check(qemu, func() error {
		_, err := exec.LookPath(qemu)
		if err != nil {
			return fmt.Errorf("not found (brew install qemu)")
		}
		return nil
	})
	check("qemu-img", func() error {
		_, err := exec.LookPath("qemu-img")
		if err != nil {
			return fmt.Errorf("not found (brew install qemu)")
		}
		return nil
	})
	if runtime.GOOS == "darwin" {
		check("hdiutil (cloud-init seed)", func() error {
			_, err := exec.LookPath("hdiutil")
			return err
		})
	}

	imgs := image.NewManager(cfg.DataDir)
	def := cfg.Image
	if def == "" {
		def = image.DefaultID()
	}
	check("base image "+def, func() error {
		if !imgs.Ready(def) {
			return fmt.Errorf("missing — run: grain image pull %s", def)
		}
		return nil
	})

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

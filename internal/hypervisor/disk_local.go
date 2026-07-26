package hypervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// LocalDisk clones base images using APFS clonefile on macOS when possible,
// otherwise falls back to copy / qemu-img.
type LocalDisk struct {
	DataDir string
}

func NewLocalDisk(dataDir string) *LocalDisk {
	return &LocalDisk{DataDir: dataDir}
}

func (d *LocalDisk) imagesDir() string {
	return filepath.Join(d.DataDir, "images")
}

// EnsureBase expects a base disk at images/<image>/disk.qcow2 or disk.img.
// For alpine-cloud we create a tiny placeholder until `grain image pull` fills it.
func (d *LocalDisk) EnsureBase(_ context.Context, image string) (string, error) {
	dir := filepath.Join(d.imagesDir(), image)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// prefer qcow2 then raw
	for _, name := range []string{"disk.qcow2", "disk.img", "disk.raw"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			return p, nil
		}
	}
	// bootstrap empty raw disk marker — real images via grain image pull
	p := filepath.Join(dir, "disk.img")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	// 1 MiB placeholder so CoW/clone paths work in tests without full images
	if err := f.Truncate(1 << 20); err != nil {
		_ = f.Close()
		return "", err
	}
	_ = f.Close()
	// write README for developers
	_ = os.WriteFile(filepath.Join(dir, "README.txt"), []byte(
		"Placeholder base disk. Run: grain image pull\n"+
			"Or place a bootable disk.img / disk.qcow2 here with kernel.\n",
	), 0o644)
	return p, nil
}

func (d *LocalDisk) Clone(ctx context.Context, baseDisk, destPath string, sizeGB int) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	// Try APFS clonefile (instant CoW on macOS)
	if runtime.GOOS == "darwin" {
		if err := clonefile(baseDisk, destPath); err == nil {
			return maybeResize(ctx, destPath, sizeGB)
		}
	}
	// qemu-img convert/create if available
	if _, err := exec.LookPath("qemu-img"); err == nil {
		cmd := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", "-F", "raw", "-b", baseDisk, destPath)
		// if base is qcow2, adjust
		if filepath.Ext(baseDisk) == ".qcow2" {
			cmd = exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", "-b", baseDisk, "-F", "qcow2", destPath)
		}
		if out, err := cmd.CombinedOutput(); err == nil {
			return maybeResize(ctx, destPath, sizeGB)
		} else {
			// fall through to copy; keep message for debug
			_ = out
		}
	}
	// plain copy
	if err := copyFile(baseDisk, destPath); err != nil {
		return err
	}
	return maybeResize(ctx, destPath, sizeGB)
}

func maybeResize(ctx context.Context, path string, sizeGB int) error {
	if sizeGB <= 0 {
		return nil
	}
	if _, err := exec.LookPath("qemu-img"); err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "qemu-img", "resize", path, fmt.Sprintf("%dG", sizeGB))
	_, _ = cmd.CombinedOutput()
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// clonefile uses cp -c on macOS (APFS copy-on-write when supported).
func clonefile(src, dst string) error {
	cmd := exec.Command("cp", "-c", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp -c: %w (%s)", err, string(out))
	}
	return nil
}

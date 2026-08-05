package hypervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cxdy/grain/internal/hostbin"
	"github.com/cxdy/grain/internal/image"
)

// LocalDisk clones base images using APFS clonefile on macOS when possible.
type LocalDisk struct {
	DataDir string
	Images  *image.Manager
}

func NewLocalDisk(dataDir string) *LocalDisk {
	return &LocalDisk{
		DataDir: dataDir,
		Images:  image.NewManager(dataDir),
	}
}

func (d *LocalDisk) EnsureBase(ctx context.Context, imageID string) (string, error) {
	if d.Images == nil {
		d.Images = image.NewManager(d.DataDir)
	}
	if !d.Images.Ready(imageID) {
		// Auto-pull when base is missing (progress nil: silent; CLI pull still prints).
		// Local-only catalog ids (e.g. grain-ubuntu) must be imported, not pulled.
		if err := d.Images.Pull(ctx, imageID, nil); err != nil {
			hint := fmt.Sprintf("try: grain image pull %s", imageID)
			if spec, gerr := image.Get(imageID); gerr == nil && spec.LocalOnly {
				hint = fmt.Sprintf("try: grain image import <path> --id %s", imageID)
			}
			return "", fmt.Errorf("image %q not ready: %w (%s)", imageID, err, hint)
		}
	}
	p, err := d.Images.DiskPath(imageID)
	if err != nil {
		return "", err
	}
	return p, nil
}

func (d *LocalDisk) Clone(ctx context.Context, baseDisk, destPath string, sizeGB int) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	// Prefer qcow2 overlay (fast, small) when qemu-img exists and base is qcow2/raw
	if qemuImg, err := hostbin.LookPath("qemu-img"); err == nil {
		format := "raw"
		if filepath.Ext(baseDisk) == ".qcow2" {
			format = "qcow2"
		}
		// write overlay as qcow2
		if filepath.Ext(destPath) != ".qcow2" {
			destPath = destPath + ".qcow2"
		}
		cmd := exec.CommandContext(ctx, qemuImg, "create", "-f", "qcow2", "-b", baseDisk, "-F", format, destPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("qemu-img create: %w (%s)", err, string(out))
		}
		return maybeResize(ctx, destPath, sizeGB)
	}

	// APFS clonefile (instant CoW on macOS for raw copies)
	if runtime.GOOS == "darwin" {
		if err := clonefile(baseDisk, destPath); err == nil {
			return maybeResize(ctx, destPath, sizeGB)
		}
	}
	if err := copyFile(baseDisk, destPath); err != nil {
		return err
	}
	return maybeResize(ctx, destPath, sizeGB)
}

// ClonePath is destPath that Clone may rewrite (qcow2 suffix).
// Callers should use the path returned... for now Manager uses fixed name;
// we keep dest as given and if qcow2 overlay, write to destPath as-is with .qcow2 content name disk.qcow2

func maybeResize(ctx context.Context, path string, sizeGB int) error {
	if sizeGB <= 0 {
		return nil
	}
	qemuImg, err := hostbin.LookPath("qemu-img")
	if err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, qemuImg, "resize", path, fmt.Sprintf("%dG", sizeGB))
	_, _ = cmd.CombinedOutput()
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func clonefile(src, dst string) error {
	cmd := exec.Command("cp", "-c", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp -c: %w (%s)", err, string(out))
	}
	return nil
}

// CopyDiskFile copies a VM root disk (qcow2 overlay or raw) to destPath.
// Prefer APFS clonefile (cp -c) on macOS, then qemu-img convert when available
// (independent image; good for overlays that should not share a writable chain),
// then plain file copy. Does not resize.
//
// For qcow2 overlays that should keep their backing file (small, fast), pass
// preferConvert=false; convert is still used as a fallback only when copy fails
// is not applicable — callers that want a bit-identical overlay should use
// preferConvert=false (default for grain clone).
func CopyDiskFile(ctx context.Context, srcPath, destPath string, preferConvert bool) error {
	if srcPath == "" || destPath == "" {
		return fmt.Errorf("copy disk: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	// Never overwrite an existing dest in place without caller cleanup.
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("destination disk already exists: %s", destPath)
	}

	if !preferConvert {
		if runtime.GOOS == "darwin" {
			if err := clonefile(srcPath, destPath); err == nil {
				return nil
			}
		}
		if err := copyFile(srcPath, destPath); err == nil {
			return nil
		} else if qemuImg, lookErr := hostbin.LookPath("qemu-img"); lookErr == nil {
			return qemuImgConvert(ctx, qemuImg, srcPath, destPath)
		} else {
			return err
		}
	}

	if qemuImg, err := hostbin.LookPath("qemu-img"); err == nil {
		if err := qemuImgConvert(ctx, qemuImg, srcPath, destPath); err == nil {
			return nil
		}
	}
	if runtime.GOOS == "darwin" {
		if err := clonefile(srcPath, destPath); err == nil {
			return nil
		}
	}
	return copyFile(srcPath, destPath)
}

func qemuImgConvert(ctx context.Context, qemuImg, src, dest string) error {
	format := "raw"
	if strings.HasSuffix(strings.ToLower(src), ".qcow2") || strings.HasSuffix(strings.ToLower(dest), ".qcow2") {
		format = "qcow2"
	}
	// Output format follows dest extension; default qcow2 when dest ends in .qcow2.
	outFmt := "raw"
	if strings.HasSuffix(strings.ToLower(dest), ".qcow2") {
		outFmt = "qcow2"
	} else if format == "qcow2" {
		// Keep qcow2 content even if dest name lacks suffix (caller chose name).
		outFmt = "qcow2"
	}
	cmd := exec.CommandContext(ctx, qemuImg, "convert", "-f", format, "-O", outFmt, src, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("qemu-img convert: %w (%s)", err, string(out))
	}
	return nil
}

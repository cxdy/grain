package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cxdy/grain/internal/hostbin"
)

// SandboxMeta is a subset of per-VM meta.json editable from Desktop.
type SandboxMeta struct {
	Name       string `json:"name"`
	Status     string `json:"status,omitempty"`
	Persistent bool   `json:"persistent"`
	CPUs       int    `json:"cpus"`
	MemoryMB   int    `json:"memory_mb"`
	DiskGB     int    `json:"disk_gb"`
	Image      string `json:"image"`
	Network    string `json:"network,omitempty"`
	Arch       string `json:"arch,omitempty"`
	GPU        string `json:"gpu,omitempty"`
	DiskPath   string `json:"disk_path,omitempty"`
}

// ReadSandboxMeta loads dataDir/vms/<name>/meta.json.
func ReadSandboxMeta(dataDir, name string) (SandboxMeta, map[string]interface{}, error) {
	var meta SandboxMeta
	path := metaPath(dataDir, name)
	b, err := os.ReadFile(path)
	if err != nil {
		return meta, nil, err
	}
	raw := map[string]interface{}{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return meta, nil, err
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, raw, err
	}
	return meta, raw, nil
}

// WriteSandboxMetaResult describes what happened after a meta save.
type WriteSandboxMetaResult struct {
	NeedsRestart bool   `json:"needs_restart"`
	DiskResized  bool   `json:"disk_resized"`
	Message      string `json:"message"`
	PrevDiskGB   int    `json:"prev_disk_gb"`
	NewDiskGB    int    `json:"new_disk_gb"`
}

// WriteSandboxMeta merges fields into meta.json and grows the disk image when
// disk_gb exceeds the current qcow2 virtual size (qemu-img resize).
// Guest filesystem grow remains a separate in-guest step.
func WriteSandboxMeta(dataDir, name string, patch SandboxMeta) (WriteSandboxMetaResult, error) {
	var res WriteSandboxMetaResult
	path := metaPath(dataDir, name)
	prev, raw, err := ReadSandboxMeta(dataDir, name)
	if err != nil {
		return res, err
	}
	res.PrevDiskGB = prev.DiskGB
	wasRunning := strings.EqualFold(fmt.Sprint(raw["status"]), "running")

	if patch.CPUs > 0 {
		raw["cpus"] = patch.CPUs
	}
	if patch.MemoryMB > 0 {
		raw["memory_mb"] = patch.MemoryMB
	}
	if patch.DiskGB > 0 {
		raw["disk_gb"] = patch.DiskGB
		res.NewDiskGB = patch.DiskGB
	}
	if patch.Image != "" {
		raw["image"] = patch.Image
	}
	if patch.Network != "" {
		raw["network"] = patch.Network
	}
	if patch.Arch != "" {
		raw["arch"] = patch.Arch
	}
	if patch.GPU != "" {
		raw["gpu"] = patch.GPU
	}
	raw["persistent"] = patch.Persistent

	// Grow backing disk when requested size exceeds actual image virtual size.
	// Compare against the real qcow2, not only previous meta (meta may already
	// have been bumped without a successful resize).
	targetGB := patch.DiskGB
	if targetGB <= 0 {
		targetGB = prev.DiskGB
	}
	if targetGB > 0 {
		diskPath := resolveDiskPath(dataDir, name, raw)
		needGrow, curGB, err := diskNeedsGrow(diskPath, targetGB)
		if err != nil && patch.DiskGB > 0 && patch.DiskGB > prev.DiskGB {
			// Only hard-fail when we were asked to grow and cannot inspect.
			return res, fmt.Errorf("disk: %w", err)
		}
		if needGrow {
			if wasRunning {
				return res, fmt.Errorf("stop the sandbox before increasing disk size (image ~%d GiB → %d GiB)", curGB, targetGB)
			}
			if err := resizeDiskGB(context.Background(), diskPath, targetGB); err != nil {
				return res, fmt.Errorf("disk resize: %w", err)
			}
			res.DiskResized = true
			res.Message = fmt.Sprintf("disk image resized to %dG (was ~%dG); grow the guest filesystem if needed (e.g. sudo growpart /dev/vdb 1 && sudo resize2fs /dev/vdb1)", targetGB, curGB)
		}
	}

	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return res, err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return res, err
	}
	res.NeedsRestart = wasRunning
	if res.Message == "" && wasRunning {
		res.Message = "meta updated; restart for CPU/memory changes"
	}
	return res, nil
}

func resolveDiskPath(dataDir, name string, raw map[string]interface{}) string {
	diskPath := strings.TrimSpace(fmt.Sprint(raw["disk_path"]))
	if diskPath != "" && diskPath != "<nil>" {
		return expandHome(diskPath)
	}
	base := filepath.Join(expandHome(dataDir), "vms", filepath.Base(name))
	for _, cand := range []string{
		filepath.Join(base, "disk.img.qcow2"),
		filepath.Join(base, "disk.qcow2"),
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return filepath.Join(base, "disk.img.qcow2")
}

// diskNeedsGrow reports whether path's virtual size is below sizeGB.
// curGB is the current virtual size in whole GiB (rounded down).
func diskNeedsGrow(path string, sizeGB int) (need bool, curGB int, err error) {
	if sizeGB <= 0 {
		return false, 0, nil
	}
	if path == "" {
		return false, 0, fmt.Errorf("disk path empty")
	}
	if _, err := os.Stat(path); err != nil {
		return false, 0, fmt.Errorf("disk %s: %w", path, err)
	}
	virt, err := diskVirtualSizeBytes(path)
	if err != nil {
		// Fallback: if we cannot inspect, assume grow is needed when meta asks.
		return true, 0, nil
	}
	curGB = int(virt / (1024 * 1024 * 1024))
	target := int64(sizeGB) * 1024 * 1024 * 1024
	return virt < target, curGB, nil
}

func diskVirtualSizeBytes(path string) (int64, error) {
	qemuImg, err := hostbin.LookPath("qemu-img")
	if err != nil {
		return 0, err
	}
	// qemu-img info --output=json is portable; avoid write-lock when possible.
	cmd := exec.Command(qemuImg, "info", "--output=json", "--force-share", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Older qemu without --force-share
		cmd = exec.Command(qemuImg, "info", "--output=json", path)
		out, err = cmd.CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	var info struct {
		VirtualSize int64 `json:"virtual-size"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return 0, err
	}
	if info.VirtualSize <= 0 {
		return 0, fmt.Errorf("no virtual-size in qemu-img info")
	}
	return info.VirtualSize, nil
}

func resizeDiskGB(ctx context.Context, path string, sizeGB int) error {
	if sizeGB <= 0 {
		return fmt.Errorf("invalid size")
	}
	if path == "" {
		return fmt.Errorf("disk path empty")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("disk %s: %w", path, err)
	}
	qemuImg, err := hostbin.LookPath("qemu-img")
	if err != nil {
		return fmt.Errorf("qemu-img not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, qemuImg, "resize", path, fmt.Sprintf("%dG", sizeGB))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func metaPath(dataDir, name string) string {
	name = filepath.Base(name)
	return filepath.Join(expandHome(dataDir), "vms", name, "meta.json")
}

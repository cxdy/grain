package desktop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSandboxMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	name := "work"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw := map[string]interface{}{
		"name": name, "status": "running", "cpus": 2, "memory_mb": 2048,
		"disk_gb": 8, "image": "grain-ubuntu", "persistent": true, "extra": "keep",
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	meta, m, err := ReadSandboxMeta(dir, name)
	if err != nil || meta.CPUs != 2 || m["extra"] != "keep" {
		t.Fatalf("%+v %v %v", meta, m, err)
	}
	res, err := WriteSandboxMeta(dir, name, SandboxMeta{CPUs: 4, MemoryMB: 4096, Persistent: true})
	if err != nil || !res.NeedsRestart {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	meta2, m2, err := ReadSandboxMeta(dir, name)
	if err != nil || meta2.CPUs != 4 || m2["extra"] != "keep" {
		t.Fatalf("%+v %v", meta2, m2)
	}
	// stopped VM → no restart needed
	raw2 := map[string]interface{}{"name": name, "status": "stopped", "cpus": 1, "memory_mb": 512, "disk_gb": 8, "persistent": false, "disk_path": filepath.Join(vmDir, "disk.qcow2")}
	b2, _ := json.Marshal(raw2)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b2, 0o600); err != nil {
		t.Fatal(err)
	}
	// create fake disk for resize path skip if no growth
	res2, err := WriteSandboxMeta(dir, name, SandboxMeta{CPUs: 2, MemoryMB: 1024, Image: "x", Network: "slirp", Arch: "arm64", GPU: "virtio", Persistent: true})
	if err != nil || res2.NeedsRestart {
		t.Fatalf("res=%+v err=%v", res2, err)
	}
	if _, _, err := ReadSandboxMeta(dir, "missing"); err == nil {
		t.Fatal("want missing error")
	}
}

func TestWriteSandboxMetaDiskResize(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img not available")
	}
	dir := t.TempDir()
	name := "diskvm"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.img.qcow2")
	// Create 1G image
	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", disk, "1G")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create disk: %v %s", err, out)
	}
	raw := map[string]interface{}{
		"name": name, "status": "stopped", "cpus": 1, "memory_mb": 512,
		"disk_gb": 1, "persistent": true, "disk_path": disk,
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := WriteSandboxMeta(dir, name, SandboxMeta{DiskGB: 2, CPUs: 1, MemoryMB: 512, Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DiskResized {
		t.Fatalf("expected disk_resized: %+v", res)
	}
	virt, err := diskVirtualSizeBytes(disk)
	if err != nil {
		t.Fatal(err)
	}
	if virt < 2*1024*1024*1024 {
		t.Fatalf("virtual size %d want >= 2GiB", virt)
	}
	// Meta already 2 but image shrunk scenario: force image small again? skip.
	// Running VM must refuse grow.
	raw2 := map[string]interface{}{
		"name": name, "status": "running", "cpus": 1, "memory_mb": 512,
		"disk_gb": 2, "persistent": true, "disk_path": disk,
	}
	b2, _ := json.Marshal(raw2)
	_ = os.WriteFile(filepath.Join(vmDir, "meta.json"), b2, 0o600)
	_, err = WriteSandboxMeta(dir, name, SandboxMeta{DiskGB: 3, Persistent: true})
	if err == nil {
		t.Fatal("want error when growing while running")
	}
}

func TestWriteSandboxMetaGrowsWhenMetaAlreadyBumped(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img not available")
	}
	dir := t.TempDir()
	name := "stale"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(vmDir, "disk.img.qcow2")
	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", disk, "1G")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create: %v %s", err, out)
	}
	// Meta claims 5G but image is still 1G (previous UI meta-only bug)
	raw := map[string]interface{}{
		"name": name, "status": "stopped", "cpus": 1, "memory_mb": 512,
		"disk_gb": 5, "persistent": true, "disk_path": disk,
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := WriteSandboxMeta(dir, name, SandboxMeta{DiskGB: 5, CPUs: 1, MemoryMB: 512, Persistent: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DiskResized {
		t.Fatalf("expected resize to catch stale meta: %+v", res)
	}
}

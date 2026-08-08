package desktop

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestWriteSandboxMetaMetricsAndResolvePath(t *testing.T) {
	dir := t.TempDir()
	name := "meta2"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// disk.qcow2 candidate for resolveDiskPath (no disk_path in meta)
	disk := filepath.Join(vmDir, "disk.qcow2")
	if err := os.WriteFile(disk, []byte("not-real"), 0o644); err != nil {
		t.Fatal(err)
	}
	// disk_gb 0 so WriteSandboxMeta skips disk grow path on non-qcow2 file
	raw := map[string]interface{}{
		"name": name, "status": "stopped", "cpus": 1, "memory_mb": 512,
		"disk_gb": 0, "persistent": false,
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := WriteSandboxMeta(dir, name, SandboxMeta{
		CPUs: 2, MemoryMB: 1024, Persistent: true,
		MetricsEnabled: true, MetricsEnabledSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	_, m, err := ReadSandboxMeta(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if m["metrics_enabled"] != true {
		t.Fatalf("metrics: %+v", m)
	}
	// resolveDiskPath helpers
	p := resolveDiskPath(dir, name, map[string]interface{}{})
	if p == "" || !strings.Contains(p, name) {
		t.Fatalf("path %q", p)
	}
	p2 := resolveDiskPath(dir, name, map[string]interface{}{"disk_path": disk})
	if p2 == "" {
		t.Fatal("empty disk path")
	}
	// missing meta write error
	if _, err := WriteSandboxMeta(dir, "nope", SandboxMeta{CPUs: 1}); err == nil {
		t.Fatal("want missing")
	}
}

func TestReadSandboxMetaInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	name := "bad"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSandboxMeta(dir, name); err == nil {
		t.Fatal("want json error")
	}
}

func TestDiskNeedsGrowAndResizeHelpers(t *testing.T) {
	need, cur, err := diskNeedsGrow("", 1)
	if err == nil || need {
		t.Fatalf("empty path: %v %v %v", need, cur, err)
	}
	need, cur, err = diskNeedsGrow("/tmp/x", 0)
	if need || err != nil || cur != 0 {
		t.Fatalf("size 0: %v %v %v", need, cur, err)
	}
	need, cur, err = diskNeedsGrow(filepath.Join(t.TempDir(), "missing.qcow2"), 2)
	if err == nil {
		t.Fatal("want missing disk error")
	}
	_ = need
	_ = cur

	if err := resizeDiskGB(context.Background(), "", 1); err == nil {
		t.Fatal("empty path")
	}
	if err := resizeDiskGB(context.Background(), "/tmp/x", 0); err == nil {
		t.Fatal("invalid size")
	}
	if err := resizeDiskGB(context.Background(), filepath.Join(t.TempDir(), "no.qcow2"), 2); err == nil {
		t.Fatal("missing disk")
	}

	// metaPath basenames
	p := metaPath("/data", "../evil")
	if strings.Contains(p, "..") {
		// filepath.Base strips parent
		if filepath.Base(p) == ".." {
			t.Fatalf("%q", p)
		}
	}
}

func TestDiskVirtualSizeAndResizeReal(t *testing.T) {
	qemuImg, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img not available")
	}
	dir := t.TempDir()
	disk := filepath.Join(dir, "d.qcow2")
	cmd := exec.Command(qemuImg, "create", "-f", "qcow2", disk, "1G")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create: %v %s", err, out)
	}
	virt, err := diskVirtualSizeBytes(disk)
	if err != nil || virt < 1024*1024*1024 {
		t.Fatalf("virt=%d err=%v", virt, err)
	}
	need, curGB, err := diskNeedsGrow(disk, 2)
	if err != nil || !need || curGB < 1 {
		t.Fatalf("need=%v cur=%d err=%v", need, curGB, err)
	}
	need, _, err = diskNeedsGrow(disk, 1)
	if err != nil || need {
		t.Fatalf("should not need grow: need=%v err=%v", need, err)
	}
	if err := resizeDiskGB(context.Background(), disk, 2); err != nil {
		t.Fatal(err)
	}
	// ensureMetaDiskGrown path
	name := "grow"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	disk2 := filepath.Join(vmDir, "disk.img.qcow2")
	cmd = exec.Command(qemuImg, "create", "-f", "qcow2", disk2, "1G")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create2: %v %s", err, out)
	}
	raw := map[string]interface{}{
		"name": name, "status": "stopped", "disk_gb": 2, "disk_path": disk2, "cpus": 1, "memory_mb": 512,
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureMetaDiskGrown(dir, name); err != nil {
		t.Fatal(err)
	}
	// no grow needed
	if err := ensureMetaDiskGrown(dir, name); err != nil {
		t.Fatal(err)
	}
	// missing meta
	if err := ensureMetaDiskGrown(dir, "nope"); err == nil {
		t.Fatal("want missing meta")
	}
	// disk_gb <= 0
	raw["disk_gb"] = 0
	b, _ = json.Marshal(raw)
	_ = os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600)
	if err := ensureMetaDiskGrown(dir, name); err != nil {
		t.Fatal(err)
	}
}

func TestWriteSandboxMetaDiskInspectHardFail(t *testing.T) {
	dir := t.TempDir()
	name := "growfail"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// no disk file; request grow beyond prev → hard fail
	raw := map[string]interface{}{
		"name": name, "status": "stopped", "cpus": 1, "memory_mb": 512,
		"disk_gb": 1, "persistent": true, "disk_path": filepath.Join(vmDir, "missing.qcow2"),
	}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := WriteSandboxMeta(dir, name, SandboxMeta{DiskGB: 5, Persistent: true})
	if err == nil {
		t.Fatal("want disk inspect error")
	}
}

func TestSandboxMetaHelpersAndEdges(t *testing.T) {
	dir := t.TempDir()
	// corrupt meta
	name := "bad"
	vmDir := filepath.Join(dir, "vms", name)
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "meta.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadSandboxMeta(dir, name); err == nil {
		t.Fatal("want corrupt error")
	}
	// resolveDiskPath variants
	base := filepath.Join(dir, "vms", "d1")
	_ = os.MkdirAll(base, 0o755)
	p := resolveDiskPath(dir, "d1", map[string]interface{}{"disk_path": ""})
	if !strings.Contains(p, "disk.img.qcow2") {
		t.Fatal(p)
	}
	p = resolveDiskPath(dir, "d1", map[string]interface{}{"disk_path": "<nil>"})
	if !strings.Contains(p, "disk.img.qcow2") {
		t.Fatal(p)
	}
	// create disk.qcow2 candidate
	qcow := filepath.Join(base, "disk.qcow2")
	if err := os.WriteFile(qcow, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	p = resolveDiskPath(dir, "d1", map[string]interface{}{})
	if p != qcow {
		t.Fatalf("want qcow got %s", p)
	}
	// explicit path with tilde-ish expand
	p = resolveDiskPath(dir, "d1", map[string]interface{}{"disk_path": qcow})
	if p != qcow {
		t.Fatal(p)
	}
	// diskNeedsGrow edges
	need, _, err := diskNeedsGrow("", 1)
	if need || err == nil {
		t.Fatal("empty path")
	}
	need, _, err = diskNeedsGrow(qcow, 0)
	if need || err != nil {
		t.Fatal(need, err)
	}
	need, _, err = diskNeedsGrow(filepath.Join(dir, "nope"), 5)
	if err == nil {
		t.Fatal("missing disk")
	}
	// fallback when cannot inspect (no qemu-img): need true
	// may succeed if qemu-img present — still exercise
	_, _, _ = diskNeedsGrow(qcow, 100)
	// resizeDiskGB edges
	if err := resizeDiskGB(context.Background(), "", 1); err == nil {
		t.Fatal("empty path")
	}
	if err := resizeDiskGB(context.Background(), qcow, 0); err == nil {
		t.Fatal("invalid size")
	}
	if err := resizeDiskGB(context.Background(), filepath.Join(dir, "no"), 2); err == nil {
		t.Fatal("missing")
	}
	// metaPath basenames
	mp := metaPath(dir, "../evil/name")
	if strings.Contains(mp, "..") {
		t.Fatal(mp)
	}
	// WriteSandboxMeta metrics + network/arch/gpu/image
	name2 := "m2"
	vm2 := filepath.Join(dir, "vms", name2)
	_ = os.MkdirAll(vm2, 0o755)
	raw := map[string]interface{}{"name": name2, "status": "stopped", "cpus": 1, "memory_mb": 512, "disk_gb": 1, "persistent": false}
	b, _ := json.Marshal(raw)
	_ = os.WriteFile(filepath.Join(vm2, "meta.json"), b, 0o600)
	res, err := WriteSandboxMeta(dir, name2, SandboxMeta{
		CPUs: 2, MemoryMB: 1024, Image: "img", Network: "overlay", Arch: "amd64", GPU: "virtio",
		Persistent: true, MetricsEnabled: true, MetricsEnabledSet: true, DiskGB: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	_, raw2, err := ReadSandboxMeta(dir, name2)
	if err != nil {
		t.Fatal(err)
	}
	if raw2["metrics_enabled"] != true {
		t.Fatalf("%v", raw2["metrics_enabled"])
	}
}

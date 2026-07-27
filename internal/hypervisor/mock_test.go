package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestMockRuntimeLifecycle(t *testing.T) {
	t.Parallel()
	m := NewMockRuntime()
	ctx := context.Background()
	dir := t.TempDir()
	disk := filepath.Join(dir, "disk.qcow2")

	inst := &vm.Instance{Name: "mock-a", DiskPath: disk}
	if m.Running(inst) {
		t.Fatal("expected not running before Start")
	}

	if err := m.Start(ctx, inst, disk); err != nil {
		t.Fatal(err)
	}
	if !m.Running(inst) {
		t.Fatal("expected running after Start")
	}
	if inst.PID != 1 {
		t.Fatalf("PID=%d", inst.PID)
	}
	if inst.IP != "127.0.0.1" {
		t.Fatalf("IP=%s", inst.IP)
	}
	if inst.SSHPort < 2200 {
		t.Fatalf("SSHPort=%d", inst.SSHPort)
	}
	if inst.AgentPort < 7475 {
		t.Fatalf("AgentPort=%d", inst.AgentPort)
	}
	wantQMP := filepath.Join(dir, QMPSocketName)
	if inst.QMPPath != wantQMP {
		t.Fatalf("QMPPath=%q want %q", inst.QMPPath, wantQMP)
	}
	if inst.Status != vm.StatusRunning {
		t.Fatalf("status=%s", inst.Status)
	}

	// Second VM gets incremented ports.
	inst2 := &vm.Instance{Name: "mock-b"}
	if err := m.Start(ctx, inst2, ""); err != nil {
		t.Fatal(err)
	}
	if inst2.SSHPort == inst.SSHPort {
		t.Fatalf("expected distinct SSH ports: %d %d", inst.SSHPort, inst2.SSHPort)
	}
	if inst2.QMPPath != "" {
		t.Fatalf("empty DiskPath should leave QMPPath empty, got %q", inst2.QMPPath)
	}

	if err := m.Stop(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if m.Running(inst) {
		t.Fatal("expected stopped")
	}
	if inst.PID != 0 || inst.QMPPath != "" || inst.Status != vm.StatusStopped {
		t.Fatalf("stop fields: pid=%d qmp=%q status=%s", inst.PID, inst.QMPPath, inst.Status)
	}
}

func TestMockRuntimeFailStart(t *testing.T) {
	t.Parallel()
	m := NewMockRuntime()
	m.FailStart = true
	inst := &vm.Instance{Name: "fail"}
	err := m.Start(context.Background(), inst, "")
	if err == nil || !strings.Contains(err.Error(), "mock start failed") {
		t.Fatalf("got %v", err)
	}
	if m.Running(inst) {
		t.Fatal("should not be alive after failed start")
	}
}

func TestMockRuntimePauseResume(t *testing.T) {
	t.Parallel()
	m := NewMockRuntime()
	ctx := context.Background()
	inst := &vm.Instance{Name: "pause-me"}

	if err := m.Pause(ctx, inst); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("pause not running: %v", err)
	}
	if err := m.Resume(ctx, inst); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("resume not running: %v", err)
	}

	if err := m.Start(ctx, inst, ""); err != nil {
		t.Fatal(err)
	}
	if m.Paused(inst.Name) {
		t.Fatal("should not be paused after start")
	}
	if err := m.Resume(ctx, inst); err == nil || !strings.Contains(err.Error(), "not paused") {
		t.Fatalf("resume not paused: %v", err)
	}

	if err := m.Pause(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if !m.Paused(inst.Name) {
		t.Fatal("expected paused")
	}
	if err := m.Pause(ctx, inst); err == nil || !strings.Contains(err.Error(), "already paused") {
		t.Fatalf("double pause: %v", err)
	}

	if err := m.Resume(ctx, inst); err != nil {
		t.Fatal(err)
	}
	if m.Paused(inst.Name) {
		t.Fatal("expected not paused after resume")
	}
}

func TestMockRuntimeSaveVM(t *testing.T) {
	t.Parallel()
	m := NewMockRuntime()
	ctx := context.Background()
	inst := &vm.Instance{Name: "snap"}

	if err := m.SaveVM(ctx, inst, "tag"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("save not running: %v", err)
	}
	if err := m.Start(ctx, inst, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveVM(ctx, inst, ""); err == nil || !strings.Contains(err.Error(), "snapshot tag") {
		t.Fatalf("empty tag: %v", err)
	}
	if err := m.SaveVM(ctx, inst, "grain-suspend"); err != nil {
		t.Fatal(err)
	}
}

func TestMockRuntimeForceDead(t *testing.T) {
	t.Parallel()
	m := NewMockRuntime()
	ctx := context.Background()
	inst := &vm.Instance{Name: "zombie"}
	if err := m.Start(ctx, inst, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Pause(ctx, inst); err != nil {
		t.Fatal(err)
	}
	m.ForceDead(inst.Name)
	if m.Running(inst) || m.Paused(inst.Name) {
		t.Fatal("ForceDead should clear alive and paused")
	}
}

func TestMockDisk(t *testing.T) {
	t.Parallel()
	d := NewMockDisk()
	ctx := context.Background()

	base, err := d.EnsureBase(ctx, "ubuntu-cloud")
	if err != nil {
		t.Fatal(err)
	}
	if base != "base:ubuntu-cloud" {
		t.Fatalf("base=%q", base)
	}

	dest := filepath.Join(t.TempDir(), "vms", "a", "disk.img")
	if err := d.Clone(ctx, base, dest, 10); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "mock-disk" {
		t.Fatalf("content=%q", b)
	}
	if d.CloneCount() != 1 {
		t.Fatalf("CloneCount=%d", d.CloneCount())
	}
	if err := d.Clone(ctx, base, filepath.Join(t.TempDir(), "b", "disk.img"), 0); err != nil {
		t.Fatal(err)
	}
	if d.CloneCount() != 2 {
		t.Fatalf("CloneCount=%d", d.CloneCount())
	}
}

func TestMockRuntimeFailFlags(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := NewMockRuntime()
	inst := &vm.Instance{Name: "ff"}
	if err := m.Start(ctx, inst, ""); err != nil {
		t.Fatal(err)
	}
	m.FailPause = true
	if err := m.Pause(ctx, inst); err == nil {
		t.Fatal("expected FailPause")
	}
	m.FailPause = false
	if err := m.Pause(ctx, inst); err != nil {
		t.Fatal(err)
	}
	m.FailResume = true
	if err := m.Resume(ctx, inst); err == nil {
		t.Fatal("expected FailResume")
	}
	m.FailResume = false
	if err := m.Resume(ctx, inst); err != nil {
		t.Fatal(err)
	}
	m.FailSaveVM = true
	if err := m.SaveVM(ctx, inst, "t"); err == nil {
		t.Fatal("expected FailSaveVM")
	}
	m.FailSaveVM = false
	m.FailStop = true
	if err := m.Stop(ctx, inst); err == nil {
		t.Fatal("expected FailStop")
	}
}

func TestMockDiskFailAndQcow2(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	d := NewMockDisk()
	d.FailEnsureBase = true
	if _, err := d.EnsureBase(ctx, "x"); err == nil {
		t.Fatal("expected FailEnsureBase")
	}
	d.FailEnsureBase = false
	d.FailClone = true
	if err := d.Clone(ctx, "base", filepath.Join(t.TempDir(), "d.img"), 1); err == nil {
		t.Fatal("expected FailClone")
	}
	d.FailClone = false
	d.WriteQcow2 = true
	dest := filepath.Join(t.TempDir(), "disk.img")
	if err := d.Clone(ctx, "base", dest, 1); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 4 || b[0] != 'Q' || b[1] != 'F' || b[2] != 'I' || b[3] != 0xfb {
		t.Fatalf("want qcow2 magic, got %q", b)
	}
}

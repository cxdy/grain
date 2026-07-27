package hypervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/cxdy/grain/internal/vm"
)

type MockRuntime struct {
	mu         sync.Mutex
	alive      map[string]bool
	paused     map[string]bool
	FailStart  bool
	FailStop   bool
	FailPause  bool
	FailResume bool
	FailSaveVM bool
}

func NewMockRuntime() *MockRuntime {
	return &MockRuntime{alive: map[string]bool{}, paused: map[string]bool{}}
}

func (m *MockRuntime) Start(_ context.Context, inst *vm.Instance, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailStart {
		return fmt.Errorf("mock start failed")
	}
	m.alive[inst.Name] = true
	delete(m.paused, inst.Name)
	inst.PID = 1
	inst.IP = "127.0.0.1"
	n := len(m.alive)
	inst.SSHPort = 2200 + n
	inst.AgentPort = 7475 + n
	if inst.DiskPath != "" {
		inst.QMPPath = filepath.Join(filepath.Dir(inst.DiskPath), QMPSocketName)
	}
	inst.Status = vm.StatusRunning
	return nil
}

func (m *MockRuntime) Stop(_ context.Context, inst *vm.Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailStop {
		return fmt.Errorf("mock stop failed")
	}
	delete(m.alive, inst.Name)
	delete(m.paused, inst.Name)
	inst.PID = 0
	inst.QMPPath = ""
	inst.Status = vm.StatusStopped
	return nil
}

func (m *MockRuntime) Pause(_ context.Context, inst *vm.Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailPause {
		return fmt.Errorf("mock pause failed")
	}
	if !m.alive[inst.Name] {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	if m.paused[inst.Name] {
		return fmt.Errorf("vm %q is already paused", inst.Name)
	}
	m.paused[inst.Name] = true
	return nil
}

func (m *MockRuntime) Resume(_ context.Context, inst *vm.Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailResume {
		return fmt.Errorf("mock resume failed")
	}
	if !m.alive[inst.Name] {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	if !m.paused[inst.Name] {
		return fmt.Errorf("vm %q is not paused", inst.Name)
	}
	delete(m.paused, inst.Name)
	return nil
}

// SaveVM is a no-op success for tests (simulates qcow2 internal snapshot).
func (m *MockRuntime) SaveVM(_ context.Context, inst *vm.Instance, tag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailSaveVM {
		return fmt.Errorf("mock savevm failed")
	}
	if !m.alive[inst.Name] {
		return fmt.Errorf("vm %q is not running", inst.Name)
	}
	if tag == "" {
		return fmt.Errorf("snapshot tag is required")
	}
	return nil
}

func (m *MockRuntime) Paused(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paused[name]
}

func (m *MockRuntime) Running(inst *vm.Instance) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[inst.Name]
}

func (m *MockRuntime) ForceDead(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
	delete(m.paused, name)
}

type MockDisk struct {
	clones         atomic.Int32
	FailEnsureBase bool
	FailClone      bool
	// WriteQcow2 also writes destPath+".qcow2" so manager qcow2 detection runs.
	WriteQcow2 bool
}

func NewMockDisk() *MockDisk { return &MockDisk{} }

func (d *MockDisk) EnsureBase(_ context.Context, imageID string) (string, error) {
	if d.FailEnsureBase {
		return "", fmt.Errorf("mock ensure base failed")
	}
	return "base:" + imageID, nil
}

func (d *MockDisk) Clone(_ context.Context, baseDisk, destPath string, sizeGB int) error {
	_ = baseDisk
	_ = sizeGB
	if d.FailClone {
		return fmt.Errorf("mock clone failed")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	payload := []byte("mock-disk")
	if d.WriteQcow2 {
		// QCOW2 magic so diskLooksQcow2(path) is true for suspend snapshot paths.
		payload = []byte{'Q', 'F', 'I', 0xfb, 0, 0, 0, 0}
	}
	if err := os.WriteFile(destPath, payload, 0o644); err != nil {
		return err
	}
	d.clones.Add(1)
	return nil
}

func (d *MockDisk) CloneCount() int { return int(d.clones.Load()) }

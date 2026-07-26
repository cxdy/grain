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

// MockRuntime is an in-process hypervisor for tests (no real VMs).
type MockRuntime struct {
	mu    sync.Mutex
	alive map[string]bool
	// FailStart forces Start to error.
	FailStart bool
}

func NewMockRuntime() *MockRuntime {
	return &MockRuntime{alive: map[string]bool{}}
}

func (m *MockRuntime) Start(_ context.Context, inst *vm.Instance, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailStart {
		return fmt.Errorf("mock start failed")
	}
	m.alive[inst.Name] = true
	inst.PID = 1
	inst.IP = "127.0.0.1"
	// allocate fake ssh ports starting at 2200
	inst.SSHPort = 2200 + len(m.alive)
	inst.Status = vm.StatusRunning
	return nil
}

func (m *MockRuntime) Stop(_ context.Context, inst *vm.Instance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, inst.Name)
	inst.PID = 0
	inst.Status = vm.StatusStopped
	return nil
}

func (m *MockRuntime) Running(inst *vm.Instance) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alive[inst.Name]
}

// ForceDead marks a VM as not running without going through Stop (crash sim).
func (m *MockRuntime) ForceDead(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alive, name)
}

// MockDisk writes tiny placeholder disk files.
type MockDisk struct {
	clones atomic.Int32
}

func NewMockDisk() *MockDisk { return &MockDisk{} }

func (d *MockDisk) EnsureBase(_ context.Context, imageID string) (string, error) {
	// synthetic base for tests (no download)
	return "base:" + imageID, nil
}

func (d *MockDisk) Clone(_ context.Context, baseDisk, destPath string, sizeGB int) error {
	_ = baseDisk
	_ = sizeGB
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(destPath, []byte("mock-disk"), 0o644); err != nil {
		return err
	}
	d.clones.Add(1)
	return nil
}

func (d *MockDisk) CloneCount() int { return int(d.clones.Load()) }

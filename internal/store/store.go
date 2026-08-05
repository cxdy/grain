package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cxdy/grain/internal/vm"
)

// Store persists VM metadata as JSON files under DataDir/vms/<name>/meta.json.
type Store struct {
	mu  sync.RWMutex
	dir string
}

func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "vms")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name, "meta.json")
}

func (s *Store) Dir(name string) string {
	return filepath.Join(s.dir, name)
}

func (s *Store) Put(inst *vm.Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.Dir(inst.Name)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(inst.Name) + ".tmp"
	// Owner-only: meta may include host paths, ports, and tags.
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(inst.Name))
}

func (s *Store) Get(name string) (*vm.Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := os.ReadFile(s.path(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("vm %q not found", name)
		}
		return nil, err
	}
	var inst vm.Instance
	if err := json.Unmarshal(b, &inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

func (s *Store) List() ([]*vm.Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*vm.Instance
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var inst vm.Instance
		if err := json.Unmarshal(b, &inst); err != nil {
			continue
		}
		out = append(out, &inst)
	}
	return out, nil
}

func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.Dir(name))
}

// Names returns a set of existing VM names.
func (s *Store) Names() (map[string]struct{}, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	m := make(map[string]struct{}, len(list))
	for _, i := range list {
		m[i.Name] = struct{}{}
	}
	return m, nil
}

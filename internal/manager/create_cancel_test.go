package manager

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/hypervisor"
	"github.com/cxdy/grain/internal/store"
	"github.com/cxdy/grain/internal/vm"
)

func TestIsContextCancel(t *testing.T) {
	t.Parallel()
	if isContextCancel(nil) {
		t.Fatal()
	}
	if !isContextCancel(context.Canceled) {
		t.Fatal("Canceled")
	}
	if !isContextCancel(errors.New("wait for grain-agent: context canceled")) {
		t.Fatal("wrapped cancel")
	}
	if isContextCancel(errors.New("timeout waiting for ssh")) {
		t.Fatal("timeout is not cancel")
	}
}

func TestListPromotesErrorWhenRunning(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "mock"
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rt := hypervisor.NewMockRuntime()
	m := New(cfg, st, rt, hypervisor.NewMockDisk(), nil)

	inst := &vm.Instance{
		Name:   "err-live",
		Status: vm.StatusError,
		Error:  "create wait canceled",
	}
	if err := rt.Start(context.Background(), inst, filepath.Join(dir, "disk")); err != nil {
		t.Fatal(err)
	}
	inst.Status = vm.StatusError
	inst.Error = "create wait canceled"
	if err := st.Put(inst); err != nil {
		t.Fatal(err)
	}

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	var found *vm.Instance
	for _, i := range list {
		if i.Name == "err-live" {
			found = i
			break
		}
	}
	if found == nil {
		t.Fatal("missing")
	}
	if found.Status != vm.StatusRunning {
		t.Fatalf("status %s want running", found.Status)
	}
	if found.Error != "" {
		t.Fatalf("error still set: %q", found.Error)
	}
}

func TestWaitAgentModeBakedAgentReady(t *testing.T) {
	srv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no port")
	}

	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "qemu"
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	imgDir := filepath.Join(dir, "images", "gold")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(imgDir, "has_agent"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := &vm.Instance{Name: "baked", AgentPort: port, IP: "127.0.0.1", Image: "gold", SSHPort: 1}
	dl := time.Now().Add(3 * time.Second)
	if err := m.waitAgentMode(context.Background(), inst, "gold", "", dl, nil, false); err != nil {
		t.Fatal(err)
	}
}

func TestWaitSSHOrAgentPrefersAgent(t *testing.T) {
	srv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no port")
	}

	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Hypervisor = "qemu"
	st, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(cfg, st, hypervisor.NewMockRuntime(), hypervisor.NewMockDisk(), nil)
	inst := &vm.Instance{Name: "race", AgentPort: port, IP: "127.0.0.1", SSHPort: 1}
	// SSH port 1 will never accept; agent should win the race.
	ok := m.waitSSHOrAgent(context.Background(), inst, "img", "", time.Now().Add(3*time.Second), nil)
	if !ok {
		t.Fatal("expected agent to win")
	}
}

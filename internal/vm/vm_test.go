package vm_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/vm"
)

func TestStatusConstants(t *testing.T) {
	t.Parallel()
	statuses := []vm.Status{
		vm.StatusCreating,
		vm.StatusRunning,
		vm.StatusPaused,
		vm.StatusSuspended,
		vm.StatusStopped,
		vm.StatusError,
	}
	for _, s := range statuses {
		if s == "" {
			t.Fatal("empty status")
		}
	}
	if vm.StatusRunning != "running" {
		t.Fatalf("StatusRunning %q", vm.StatusRunning)
	}
}

func TestWaitModeConstants(t *testing.T) {
	t.Parallel()
	if vm.WaitSSH != "ssh" || vm.WaitAgent != "agent" || vm.WaitUserdata != "userdata" {
		t.Fatalf("wait modes ssh=%q agent=%q userdata=%q", vm.WaitSSH, vm.WaitAgent, vm.WaitUserdata)
	}
}

func TestPhaseConstants(t *testing.T) {
	t.Parallel()
	phases := []string{
		vm.PhaseImage, vm.PhaseDisk, vm.PhaseSeed, vm.PhaseQEMU,
		vm.PhaseWaitSSH, vm.PhaseWaitAgent, vm.PhaseUserdata,
		vm.PhaseReady, vm.PhaseError,
	}
	seen := map[string]bool{}
	for _, p := range phases {
		if p == "" {
			t.Fatal("empty phase")
		}
		if seen[p] {
			t.Fatalf("duplicate phase %q", p)
		}
		seen[p] = true
	}
}

func TestInstanceJSONRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	inst := &vm.Instance{
		Name:       "box",
		Status:     vm.StatusRunning,
		Persistent: true,
		CPUs:       2,
		MemoryMB:   1024,
		DiskGB:     8,
		Image:      "ubuntu-cloud",
		SSHPort:    2222,
		AgentPort:  7475,
		AgentCID:   3,
		Forwards:   []vm.PortForward{{HostPort: 8080, GuestPort: 80, Proto: "tcp"}},
		LiveForwards: []vm.LiveForward{
			{HostPort: 18080, GuestPort: 80, PID: 9},
		},
		Mounts: []vm.Mount{{Host: "/tmp/h", Guest: "/mnt", Tag: "grain0"}},
		SocketForwards: []vm.SocketForward{
			{HostPath: "/tmp/d.sock", GuestPath: "/var/run/docker.sock", PID: 1},
		},
		Arch:      "arm64",
		GPU:       "virtio",
		Network:   "slirp",
		Tags:      map[string]string{"env": "test"},
		CreatedAt: now,
		DiskPath:  "/data/disk.img",
		PID:       42,
		QMPPath:   "/data/qmp.sock",
	}
	b, err := json.Marshal(inst)
	if err != nil {
		t.Fatal(err)
	}
	var got vm.Instance
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "box" || got.Status != vm.StatusRunning || got.SSHPort != 2222 {
		t.Fatalf("got %+v", got)
	}
	if len(got.Forwards) != 1 || got.Forwards[0].GuestPort != 80 {
		t.Fatalf("forwards %+v", got.Forwards)
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Tag != "grain0" {
		t.Fatalf("mounts %+v", got.Mounts)
	}
	if got.Arch != "arm64" || got.GPU != "virtio" {
		t.Fatalf("arch/gpu %s/%s", got.Arch, got.GPU)
	}
	// LoadVM is not serialized
	inst.LoadVM = "suspend0"
	b2, _ := json.Marshal(inst)
	var got2 vm.Instance
	_ = json.Unmarshal(b2, &got2)
	if got2.LoadVM != "" {
		t.Fatal("LoadVM should not be in JSON")
	}
}

func TestCreateEventJSON(t *testing.T) {
	t.Parallel()
	ev := vm.CreateEvent{
		Phase:   vm.PhaseReady,
		Message: "ready",
		Name:    "sbox-1",
		SSHPort: 2201,
		Instance: &vm.Instance{
			Name:   "sbox-1",
			Status: vm.StatusRunning,
		},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var got vm.CreateEvent
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Phase != vm.PhaseReady || got.Instance == nil || got.Instance.Name != "sbox-1" {
		t.Fatalf("got %+v", got)
	}

	errEv := vm.CreateEvent{Phase: vm.PhaseError, Error: "boom"}
	b, _ = json.Marshal(errEv)
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "boom" {
		t.Fatalf("error %q", got.Error)
	}
}

func TestCreateOptsOnEventNotSerialized(t *testing.T) {
	t.Parallel()
	// CreateOpts is not a JSON API type; just exercise field assignment.
	called := false
	opts := vm.CreateOpts{
		Name:        "x",
		Persistent:  false,
		WaitMode:    vm.WaitAgent,
		WaitTimeout: time.Minute,
		OnEvent: func(ev vm.CreateEvent) {
			called = true
			_ = ev
		},
	}
	if opts.OnEvent == nil {
		t.Fatal("OnEvent nil")
	}
	opts.OnEvent(vm.CreateEvent{Phase: vm.PhaseImage})
	if !called {
		t.Fatal("OnEvent not called")
	}
	if opts.WaitMode != vm.WaitAgent {
		t.Fatalf("WaitMode %q", opts.WaitMode)
	}
}

func TestPortForwardLiveForwardSocketMount(t *testing.T) {
	t.Parallel()
	pf := vm.PortForward{HostPort: 0, GuestPort: 443}
	b, _ := json.Marshal(pf)
	var pf2 vm.PortForward
	_ = json.Unmarshal(b, &pf2)
	if pf2.GuestPort != 443 {
		t.Fatalf("%+v", pf2)
	}

	lf := vm.LiveForward{HostPort: 9, GuestPort: 80, PID: 1}
	b, _ = json.Marshal(lf)
	var lf2 vm.LiveForward
	_ = json.Unmarshal(b, &lf2)
	if lf2.PID != 1 {
		t.Fatalf("%+v", lf2)
	}

	sf := vm.SocketForward{HostPath: "/h", GuestPath: "/g"}
	b, _ = json.Marshal(sf)
	var sf2 vm.SocketForward
	_ = json.Unmarshal(b, &sf2)
	if sf2.HostPath != "/h" {
		t.Fatalf("%+v", sf2)
	}

	m := vm.Mount{Host: "/host", Guest: "/guest"}
	b, _ = json.Marshal(m)
	var m2 vm.Mount
	_ = json.Unmarshal(b, &m2)
	if m2.Guest != "/guest" {
		t.Fatalf("%+v", m2)
	}
}

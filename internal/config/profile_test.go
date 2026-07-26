package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestLookupProfileUnknown(t *testing.T) {
	t.Parallel()
	c := config.Defaults()
	_, err := c.LookupProfile("nope")
	if err == nil {
		t.Fatal("expected unknown profile error")
	}
	if got := err.Error(); got != `unknown profile "nope"` {
		t.Fatalf("err %q", got)
	}
}

func TestResolveCreateMergesFlagOverProfileOverZero(t *testing.T) {
	t.Parallel()
	c := config.Defaults()
	c.Profiles = map[string]config.Profile{
		"agent": {
			CPUs:       4,
			MemoryMB:   4096,
			DiskGB:     20,
			Image:      "ubuntu-cloud",
			Persistent: true,
			Preset:     "docker",
			Mounts:     []config.ProfileMount{{Host: ".", Guest: "/work"}},
			Forwards:   []config.ProfileForward{{GuestPort: 3000}},
		},
	}

	// No flags: full profile
	r, err := c.ResolveCreate("agent", config.CreateOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ProfileName != "agent" {
		t.Fatalf("profile name %q", r.ProfileName)
	}
	if r.CPUs != 4 || r.MemoryMB != 4096 || r.DiskGB != 20 {
		t.Fatalf("resources cpus=%d mem=%d disk=%d", r.CPUs, r.MemoryMB, r.DiskGB)
	}
	if r.Image != "ubuntu-cloud" || !r.Persistent || r.Preset != "docker" {
		t.Fatalf("image=%q persist=%v preset=%q", r.Image, r.Persistent, r.Preset)
	}
	if len(r.Mounts) != 1 || r.Mounts[0].Guest != "/work" {
		t.Fatalf("mounts %+v", r.Mounts)
	}
	if len(r.Forwards) != 1 || r.Forwards[0].GuestPort != 3000 {
		t.Fatalf("forwards %+v", r.Forwards)
	}

	// Explicit flags override profile
	r, err = c.ResolveCreate("agent", config.CreateOverrides{
		CPUs:          8,
		CPUsSet:       true,
		MemoryMB:      1024,
		MemoryMBSet:   true,
		DiskGB:        5,
		DiskGBSet:     true,
		Image:         "other",
		ImageSet:      true,
		Persistent:    false,
		PersistentSet: true,
		Preset:        "k3s",
		PresetSet:     true,
		ForwardsSet:   true, // CLI publish present → drop profile forwards
		MountsSet:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.CPUs != 8 || r.MemoryMB != 1024 || r.DiskGB != 5 {
		t.Fatalf("flag resources cpus=%d mem=%d disk=%d", r.CPUs, r.MemoryMB, r.DiskGB)
	}
	if r.Image != "other" || r.Persistent || r.Preset != "k3s" {
		t.Fatalf("flag image=%q persist=%v preset=%q", r.Image, r.Persistent, r.Preset)
	}
	if len(r.Mounts) != 0 || len(r.Forwards) != 0 {
		t.Fatalf("want no profile mounts/forwards when CLI set, got mounts=%+v forwards=%+v", r.Mounts, r.Forwards)
	}
}

func TestResolveCreateUnknownProfile(t *testing.T) {
	t.Parallel()
	c := config.Defaults()
	_, err := c.ResolveCreate("missing", config.CreateOverrides{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveCreateNoProfile(t *testing.T) {
	t.Parallel()
	c := config.Defaults()
	r, err := c.ResolveCreate("", config.CreateOverrides{
		CPUs:    2,
		CPUsSet: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.ProfileName != "" || r.CPUs != 2 || r.MemoryMB != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestLoadProfilesYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte(`
profiles:
  agent:
    cpus: 4
    memory_mb: 4096
    disk_gb: 20
    image: ubuntu-cloud
    persistent: false
    preset: ""
    mounts:
      - {host: ".", guest: "/work"}
    forwards:
      - {guest_port: 3000}
  lab:
    cpus: 2
    memory_mb: 2048
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	names := c.ProfileNames()
	if len(names) != 2 || names[0] != "agent" || names[1] != "lab" {
		t.Fatalf("names %v", names)
	}
	p, err := c.LookupProfile("agent")
	if err != nil {
		t.Fatal(err)
	}
	if p.CPUs != 4 || p.MemoryMB != 4096 || len(p.Mounts) != 1 || p.Mounts[0].Host != "." {
		t.Fatalf("%+v", p)
	}
	if len(p.Forwards) != 1 || p.Forwards[0].GuestPort != 3000 {
		t.Fatalf("forwards %+v", p.Forwards)
	}
}

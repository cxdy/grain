package cli

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestProfileSummary(t *testing.T) {
	t.Parallel()
	if got := profileSummary("empty", config.Profile{}); got != "empty" {
		t.Fatalf("empty profile: %q", got)
	}
	p := config.Profile{
		CPUs:     2,
		MemoryMB: 2048,
		DiskGB:   10,
		Image:    "grain-ubuntu",
		Preset:   "act",
		Mounts:   []config.ProfileMount{{Host: "/tmp", Guest: "/mnt"}},
		Forwards: []config.ProfileForward{{HostPort: 8080, GuestPort: 80}},
	}
	got := profileSummary("dev", p)
	for _, want := range []string{"dev", "cpus=2", "mem=2048", "disk=10", "image=grain-ubuntu", "preset=act", "mounts=1", "fwds=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q missing %q", got, want)
		}
	}
}

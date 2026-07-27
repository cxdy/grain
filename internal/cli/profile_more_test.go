package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestProfileMountsToVM(t *testing.T) {
	t.Parallel()
	if profileMountsToVM(nil) != nil {
		t.Fatal("nil input")
	}
	out := profileMountsToVM([]config.ProfileMount{{Host: "/h", Guest: "/g"}})
	if len(out) != 1 || out[0].Host != "/h" || out[0].Guest != "/g" {
		t.Fatalf("%+v", out)
	}
}

func TestProfileForwardsToVM(t *testing.T) {
	t.Parallel()
	if profileForwardsToVM(nil) != nil {
		t.Fatal("nil input")
	}
	out := profileForwardsToVM([]config.ProfileForward{
		{HostPort: 8080, GuestPort: 80},
		{HostPort: 53, GuestPort: 53, Proto: "udp"},
	})
	if len(out) != 2 {
		t.Fatalf("len %d", len(out))
	}
	if out[0].Proto != "tcp" {
		t.Fatalf("default proto %q", out[0].Proto)
	}
	if out[1].Proto != "udp" || out[1].GuestPort != 53 {
		t.Fatalf("%+v", out[1])
	}
}

func TestCmdProfileLs(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")

	// empty profiles
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf("data_dir: %q\n", dir)), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdProfileLs(&cfgPath)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("empty: %v", err)
	}

	// with profiles
	content := fmt.Sprintf(`data_dir: %q
profiles:
  dev:
    cpus: 2
    memory_mb: 2048
    disk_gb: 10
    image: grain-ubuntu
    preset: docker
    persistent: true
    mounts:
      - host: /tmp
        guest: /mnt
    forwards:
      - host_port: 8080
        guest_port: 80
  bare: {}
`, dir)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = cmdProfileLs(&cfgPath)
	// Capture is stdout print — just ensure no error.
	if err := cmd.Execute(); err != nil {
		t.Fatalf("with profiles: %v", err)
	}
}

func TestProfileSummaryPartial(t *testing.T) {
	t.Parallel()
	got := profileSummary("x", config.Profile{CPUs: 1})
	if !strings.Contains(got, "cpus=1") || !strings.HasPrefix(got, "x") {
		t.Fatalf("%q", got)
	}
}

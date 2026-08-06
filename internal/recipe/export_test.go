package recipe

import (
	"strings"
	"testing"
)

func TestFromSnapshotMinimal(t *testing.T) {
	t.Parallel()
	f, err := FromSnapshot(Snapshot{
		Name:       "work",
		Image:      "grain-ubuntu",
		CPUs:       4,
		MemoryMB:   8192,
		DiskGB:     55,
		Persistent: true,
		Network:    "slirp", // should be omitted
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Metadata.Name != "work" {
		t.Fatalf("name %q", f.Metadata.Name)
	}
	if f.Spec.Network != "" {
		t.Fatalf("expected slirp omitted, got %q", f.Spec.Network)
	}
	if !f.Spec.Persistent || f.Spec.CPUs != 4 || f.Spec.MemoryMB != 8192 || f.Spec.DiskGB != 55 {
		t.Fatalf("%+v", f.Spec)
	}
	y, err := f.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	s := string(y)
	if !strings.Contains(s, "apiVersion: grain/v1") || !strings.Contains(s, "kind: Sandbox") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "grain new --recipe") {
		t.Fatal("missing header", s)
	}
	if strings.Contains(s, "network:") {
		t.Fatal("slirp should not appear", s)
	}
	// Round-trip through Parse
	f2, err := Parse(y)
	if err != nil {
		t.Fatal(err)
	}
	if f2.Metadata.Name != "work" || f2.Spec.Image != "grain-ubuntu" {
		t.Fatalf("%+v", f2)
	}
	c, err := f2.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "work" || c.CPUs != 4 || !c.Persistent {
		t.Fatalf("%+v", c)
	}
}

func TestFromSnapshotMountsAndForwards(t *testing.T) {
	t.Parallel()
	f, err := FromSnapshot(Snapshot{
		Name:  "node",
		Image: "grain-ubuntu",
		CPUs:  2,
		Mounts: []Mount{
			{Host: "/Users/cody/src/app", Guest: "/work", Tag: "grain0"},
			{Host: "", Guest: "/skip"}, // dropped
		},
		Forwards: []Forward{
			{HostPort: 0, GuestPort: 3000},
			{HostPort: 8080, GuestPort: 80, Proto: "tcp"},
			{GuestPort: 0}, // dropped
		},
		SocketForwards: []SocketForward{
			{HostPath: "/tmp/docker.sock", GuestPath: "/var/run/docker.sock"},
		},
		Arch:    "arm64",
		GPU:     "virtio",
		Network: "overlay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Spec.Mounts) != 1 || f.Spec.Mounts[0].Guest != "/work" {
		t.Fatalf("mounts %+v", f.Spec.Mounts)
	}
	if len(f.Spec.Forwards) != 2 {
		t.Fatalf("forwards %+v", f.Spec.Forwards)
	}
	// host 0 omitted
	if f.Spec.Forwards[0].HostPort != 0 || f.Spec.Forwards[0].GuestPort != 3000 {
		t.Fatalf("fwd0 %+v", f.Spec.Forwards[0])
	}
	if f.Spec.Forwards[1].HostPort != 8080 || f.Spec.Forwards[1].GuestPort != 80 {
		t.Fatalf("fwd1 %+v", f.Spec.Forwards[1])
	}
	if f.Spec.Network != "overlay" || f.Spec.Arch != "arm64" || f.Spec.GPU != "virtio" {
		t.Fatalf("%+v", f.Spec)
	}
	if len(f.Spec.SocketForwards) != 1 {
		t.Fatalf("sockets %+v", f.Spec.SocketForwards)
	}
	y, err := FormatSnapshot(Snapshot{Name: "x", Image: "grain-ubuntu", CPUs: 1})
	if err != nil || !strings.Contains(y, "name: x") {
		t.Fatalf("%v %s", err, y)
	}
}

func TestFromSnapshotRequiresName(t *testing.T) {
	t.Parallel()
	if _, err := FromSnapshot(Snapshot{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := (*File)(nil).MarshalYAML(); err == nil {
		t.Fatal("expected error")
	}
}

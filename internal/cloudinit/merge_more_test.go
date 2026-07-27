package cloudinit

import (
	"strings"
	"testing"
)

func TestBaseUserDataMap(t *testing.T) {
	t.Parallel()
	m := BaseUserData("host1", "  ssh-ed25519 AAAA k  ")
	if m["hostname"] != "host1" {
		t.Fatalf("hostname %v", m["hostname"])
	}
	if m["fqdn"] != "host1.local" {
		t.Fatalf("fqdn %v", m["fqdn"])
	}
	if m["ssh_pwauth"] != false {
		t.Fatal("ssh_pwauth")
	}
	users, ok := m["users"].([]any)
	if !ok || len(users) < 2 {
		t.Fatalf("users %#v", m["users"])
	}
	// grain user entry
	gu := grainUserEntry("ssh-ed25519 AAAA k")
	if gu["name"] != "grain" {
		t.Fatalf("%v", gu["name"])
	}
	cmd := sshAuthRuncmd("ssh-ed25519 AAAA k")
	if !strings.Contains(cmd, "ssh-ed25519 AAAA k") {
		t.Fatalf("runcmd %s", cmd)
	}
}

func TestIsCloudConfig(t *testing.T) {
	t.Parallel()
	if !isCloudConfig("#cloud-config\nhostname: x\n") {
		t.Fatal("expected cloud-config")
	}
	if !isCloudConfig("\n\n  #cloud-config\n") {
		t.Fatal("blank lines then header")
	}
	if isCloudConfig("#!/bin/bash\necho hi\n") {
		t.Fatal("shebang is not cloud-config")
	}
	if isCloudConfig("") {
		t.Fatal("empty")
	}
	if isCloudConfig("\n\n") {
		t.Fatal("whitespace only")
	}
}

func TestParseCloudConfigEmptyAndNil(t *testing.T) {
	t.Parallel()
	m, err := parseCloudConfig("")
	if err != nil || m == nil {
		t.Fatalf("%v %v", m, err)
	}
	// YAML null document
	m, err = parseCloudConfig("#cloud-config\n")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("nil map")
	}
}

func TestParseCloudConfigInvalid(t *testing.T) {
	t.Parallel()
	_, err := parseCloudConfig(":\n  - bad: [\n")
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMergeUserDataInvalidBase(t *testing.T) {
	t.Parallel()
	_, err := MergeUserData(":\n  [\n", "")
	if err == nil {
		t.Fatal("expected base parse error")
	}
}

func TestMergeUserDataInvalidExtraCloudConfig(t *testing.T) {
	t.Parallel()
	base := "#cloud-config\nhostname: x\n"
	_, err := MergeUserData(base, "#cloud-config\n:\n  [\n")
	if err == nil {
		t.Fatal("expected extra parse error")
	}
}

func TestToAnySliceAndAppendAny(t *testing.T) {
	t.Parallel()
	if toAnySlice(nil) != nil {
		t.Fatal("nil")
	}
	ss := toAnySlice([]string{"a", "b"})
	if len(ss) != 2 || ss[0] != "a" {
		t.Fatalf("%v", ss)
	}
	// scalar → one-element list
	one := toAnySlice("solo")
	if len(one) != 1 || one[0] != "solo" {
		t.Fatalf("%v", one)
	}
	// base nil, extra scalar → extra becomes one-element list via toAnySlice
	got := appendAny(nil, "x")
	list, ok := got.([]any)
	if !ok || len(list) != 1 || list[0] != "x" {
		t.Fatalf("got %T %#v", got, got)
	}
	// both nil → return extraVal as-is
	got = appendAny(nil, nil)
	if got != nil {
		t.Fatalf("%v", got)
	}
	// merge lists
	merged := appendAny([]any{"a"}, []any{"b"})
	ml := merged.([]any)
	if len(ml) != 2 || ml[0] != "a" || ml[1] != "b" {
		t.Fatalf("%v", ml)
	}
}

func TestRenderUserDataSkipsEmptyMounts(t *testing.T) {
	t.Parallel()
	got, err := RenderUserData("h", "ssh-ed25519 AAAA k", "",
		MountSpec{Tag: "", Guest: "/x"},
		MountSpec{Tag: "t", Guest: ""},
		MountSpec{Tag: "ok", Guest: "/mnt"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, MountRuncmd("ok", "/mnt", "9p")) {
		t.Fatalf("missing valid mount:\n%s", got)
	}
	// empty tag/guest should not produce mount -t for guest /x alone as a mount of tag ""
	if strings.Contains(got, "mount -t 9p -o trans=virtio,version=9p2000.L  /x") {
		t.Fatalf("should skip empty tag:\n%s", got)
	}
}

func TestRenderUserDataMinimalDropsModulesWithExtra(t *testing.T) {
	t.Parallel()
	// no extra keeps modules
	base := BaseUserDataMinimal("h", "ssh-ed25519 AAAA k")
	if _, ok := base["cloud_config_modules"]; !ok {
		t.Fatal("expected modules on base map")
	}
	got, err := RenderUserDataMinimal("h", "ssh-ed25519 AAAA k", "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "cloud_config_modules") {
		t.Fatalf("extra should drop cloud_config_modules:\n%s", got)
	}
	if !strings.Contains(got, "echo hi") {
		t.Fatalf("missing extra:\n%s", got)
	}
}

func TestMarshalCloudConfig(t *testing.T) {
	t.Parallel()
	s, err := marshalCloudConfig(map[string]any{"hostname": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "#cloud-config\n") {
		t.Fatalf("%s", s)
	}
}

package cloudinit_test

import (
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/cloudinit"
	"gopkg.in/yaml.v3"
)

func TestMergeUserData_ShellExtraAsRuncmd(t *testing.T) {
	base := `#cloud-config
hostname: sbox-1
runcmd:
  - [ sh, -c, "echo grain-ready > /var/lib/grain-ready" ]
`
	got, err := cloudinit.MergeUserData(base, "apt-get update && apt-get install -y curl")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "#cloud-config\n") {
		t.Fatalf("missing #cloud-config header:\n%s", got)
	}
	// Single document — no second #cloud-config or bare top-level runcmd dump.
	if strings.Count(got, "#cloud-config") != 1 {
		t.Fatalf("expected one #cloud-config document, got:\n%s", got)
	}
	doc := mustParse(t, got)
	rc, ok := doc["runcmd"].([]any)
	if !ok {
		t.Fatalf("runcmd type %T", doc["runcmd"])
	}
	if len(rc) != 2 {
		t.Fatalf("runcmd len %d want 2: %#v", len(rc), rc)
	}
	last, ok := rc[1].(string)
	if !ok || last != "apt-get update && apt-get install -y curl" {
		t.Fatalf("last runcmd = %#v", rc[1])
	}
	if doc["hostname"] != "sbox-1" {
		t.Fatalf("hostname %v", doc["hostname"])
	}
}

func TestMergeUserData_CloudConfigPackagesMerge(t *testing.T) {
	base := `#cloud-config
hostname: sbox-1
packages:
  - ca-certificates
runcmd:
  - echo base
`
	extra := `#cloud-config
packages:
  - curl
  - jq
runcmd:
  - echo extra
write_files:
  - path: /etc/motd
    content: hi
`
	got, err := cloudinit.MergeUserData(base, extra)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "#cloud-config") != 1 {
		t.Fatalf("duplicate cloud-config documents:\n%s", got)
	}
	// Must not look like naive append of a second top-level runcmd key.
	if strings.Contains(got, "runcmd:\n  - echo base\nruncmd:") {
		t.Fatalf("duplicate top-level runcmd keys:\n%s", got)
	}
	doc := mustParse(t, got)
	pkgs := toStrings(t, doc["packages"])
	if len(pkgs) != 3 || pkgs[0] != "ca-certificates" || pkgs[1] != "curl" || pkgs[2] != "jq" {
		t.Fatalf("packages = %#v", pkgs)
	}
	rc := toStrings(t, doc["runcmd"])
	if len(rc) != 2 || rc[0] != "echo base" || rc[1] != "echo extra" {
		t.Fatalf("runcmd = %#v", rc)
	}
	wf, ok := doc["write_files"].([]any)
	if !ok || len(wf) != 1 {
		t.Fatalf("write_files = %#v", doc["write_files"])
	}
	if doc["hostname"] != "sbox-1" {
		t.Fatalf("hostname %v", doc["hostname"])
	}
}

func TestMergeUserData_EmptyExtra(t *testing.T) {
	base := `#cloud-config
hostname: only
`
	got, err := cloudinit.MergeUserData(base, "")
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParse(t, got)
	if doc["hostname"] != "only" {
		t.Fatalf("%v", doc)
	}
}

func TestMergeUserData_UsersAppend(t *testing.T) {
	base := `#cloud-config
users:
  - default
  - name: grain
`
	extra := `#cloud-config
users:
  - name: alice
    sudo: ALL=(ALL) NOPASSWD:ALL
`
	got, err := cloudinit.MergeUserData(base, extra)
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParse(t, got)
	users, ok := doc["users"].([]any)
	if !ok || len(users) != 3 {
		t.Fatalf("users = %#v", doc["users"])
	}
}

func TestMergeUserData_ScalarOverride(t *testing.T) {
	base := `#cloud-config
hostname: base
ssh_pwauth: false
`
	extra := `#cloud-config
hostname: overridden
`
	got, err := cloudinit.MergeUserData(base, extra)
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParse(t, got)
	if doc["hostname"] != "overridden" {
		t.Fatalf("hostname %v", doc["hostname"])
	}
	if doc["ssh_pwauth"] != false {
		t.Fatalf("ssh_pwauth %v", doc["ssh_pwauth"])
	}
}

func TestMergeUserData_ShebangIsShellNotCloudConfig(t *testing.T) {
	base := `#cloud-config
runcmd:
  - echo base
`
	script := "#!/bin/bash\necho hello\n"
	got, err := cloudinit.MergeUserData(base, script)
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParse(t, got)
	rc, ok := doc["runcmd"].([]any)
	if !ok || len(rc) != 2 {
		t.Fatalf("runcmd = %#v", doc["runcmd"])
	}
	// Extra should be the full script string, not parsed as YAML.
	if _, isMap := rc[1].(map[string]any); isMap {
		t.Fatalf("shebang script was parsed as cloud-config: %#v", rc[1])
	}
}

func TestRenderUserData_IncludesGrainReadyAndSSH(t *testing.T) {
	got, err := cloudinit.RenderUserData("sbox-9", "ssh-ed25519 AAAA test@grain", "echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "grain-ready") {
		t.Fatalf("missing grain-ready:\n%s", got)
	}
	if !strings.Contains(got, "ssh-ed25519 AAAA test@grain") {
		t.Fatalf("missing ssh key:\n%s", got)
	}
	if !strings.Contains(got, "name: grain") {
		t.Fatalf("missing grain user:\n%s", got)
	}
	if !strings.Contains(got, "echo hi") {
		t.Fatalf("missing shell extra:\n%s", got)
	}
	doc := mustParse(t, got)
	rc, ok := doc["runcmd"].([]any)
	if !ok || len(rc) < 3 {
		t.Fatalf("expected base runcmds + extra, got %#v", doc["runcmd"])
	}
}

func TestRenderUserData_MountRuncmds(t *testing.T) {
	got, err := cloudinit.RenderUserData("sbox-1", "ssh-ed25519 AAAA k", "",
		cloudinit.MountSpec{Tag: "grain0", Guest: "/mnt/src"},
		cloudinit.MountSpec{Tag: "grain1", Guest: "/data"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want0 := cloudinit.MountRuncmd("grain0", "/mnt/src", "9p")
	want1 := cloudinit.MountRuncmd("grain1", "/data", "")
	if !strings.Contains(got, want0) {
		t.Fatalf("missing mount0 %q in:\n%s", want0, got)
	}
	if !strings.Contains(got, want1) {
		t.Fatalf("missing mount1 %q in:\n%s", want1, got)
	}
	if !strings.Contains(got, "trans=virtio,version=9p2000.L") {
		t.Fatalf("missing 9p options:\n%s", got)
	}
}

func TestRenderUserDataMinimal_HostnameKeysAndMarkers(t *testing.T) {
	key := "ssh-ed25519 AAAA test@grain"
	got, err := cloudinit.RenderUserDataMinimal("clone-1", key, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "#cloud-config\n") {
		t.Fatalf("missing #cloud-config header:\n%s", got)
	}
	doc := mustParse(t, got)
	if doc["hostname"] != "clone-1" {
		t.Fatalf("hostname %v", doc["hostname"])
	}
	if doc["fqdn"] != "clone-1.local" {
		t.Fatalf("fqdn %v", doc["fqdn"])
	}
	if doc["ssh_pwauth"] != false {
		t.Fatalf("ssh_pwauth %v", doc["ssh_pwauth"])
	}
	if doc["package_update"] != false || doc["package_upgrade"] != false {
		t.Fatalf("expected package_update/upgrade false: update=%v upgrade=%v",
			doc["package_update"], doc["package_upgrade"])
	}
	if !strings.Contains(got, key) {
		t.Fatalf("missing ssh key:\n%s", got)
	}
	if !strings.Contains(got, "name: grain") {
		t.Fatalf("missing grain user:\n%s", got)
	}
	if !strings.Contains(got, "/var/lib/grain/userdata-ran") {
		t.Fatalf("missing userdata-ran marker:\n%s", got)
	}
	if !strings.Contains(got, "grain-ready") {
		t.Fatalf("missing grain-ready:\n%s", got)
	}
	// Full seed should still work and not include package_update false by default.
	full, err := cloudinit.RenderUserData("sbox-9", key, "")
	if err != nil {
		t.Fatal(err)
	}
	fullDoc := mustParse(t, full)
	if fullDoc["hostname"] != "sbox-9" {
		t.Fatalf("full hostname %v", fullDoc["hostname"])
	}
	if _, has := fullDoc["package_update"]; has {
		t.Fatalf("full seed should not set package_update: %#v", fullDoc["package_update"])
	}
	if !strings.Contains(full, "name: grain") || !strings.Contains(full, key) {
		t.Fatalf("full seed missing user/key:\n%s", full)
	}
}

func TestRenderUserDataMinimal_MountsAndExtra(t *testing.T) {
	got, err := cloudinit.RenderUserDataMinimal("m", "ssh-ed25519 AAAA k", "echo extra",
		cloudinit.MountSpec{Tag: "grain0", Guest: "/work"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, cloudinit.MountRuncmd("grain0", "/work", "9p")) {
		t.Fatalf("missing mount runcmd:\n%s", got)
	}
	if !strings.Contains(got, "echo extra") {
		t.Fatalf("missing shell extra:\n%s", got)
	}
}

func TestBaseUserDataMinimal_Map(t *testing.T) {
	m := cloudinit.BaseUserDataMinimal("h1", "ssh-ed25519 BBBB k")
	if m["hostname"] != "h1" {
		t.Fatalf("hostname %v", m["hostname"])
	}
	mods, ok := m["cloud_config_modules"].([]any)
	if !ok || len(mods) == 0 {
		t.Fatalf("cloud_config_modules = %#v", m["cloud_config_modules"])
	}
	// Minimal must not grow packages list.
	if _, has := m["packages"]; has {
		t.Fatalf("minimal should not set packages")
	}
}

func TestMountRuncmd(t *testing.T) {
	got := cloudinit.MountRuncmd("grain0", "/work", "9p")
	want := "mkdir -p /work && mount -t 9p -o trans=virtio,version=9p2000.L grain0 /work"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// empty driver defaults to 9p
	if got := cloudinit.MountRuncmd("grain0", "/work", ""); got != want {
		t.Fatalf("empty driver: got %q want %q", got, want)
	}
	vfs := cloudinit.MountRuncmd("grain0", "/work", "virtiofs")
	wantVFS := "mkdir -p /work && mount -t virtiofs grain0 /work"
	if vfs != wantVFS {
		t.Fatalf("virtiofs: got %q want %q", vfs, wantVFS)
	}
}

func TestRenderUserData_MountRuncmdsVirtiofs(t *testing.T) {
	got, err := cloudinit.RenderUserData("sbox-1", "ssh-ed25519 AAAA k", "",
		cloudinit.MountSpec{Tag: "grain0", Guest: "/mnt/src", Driver: "virtiofs"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := cloudinit.MountRuncmd("grain0", "/mnt/src", "virtiofs")
	if !strings.Contains(got, want) {
		t.Fatalf("missing virtiofs mount %q in:\n%s", want, got)
	}
	if strings.Contains(got, "mount -t 9p") {
		t.Fatalf("should not inject 9p mount for virtiofs driver:\n%s", got)
	}
}

func mustParse(t *testing.T, cloudConfig string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := yaml.Unmarshal([]byte(cloudConfig), &m); err != nil {
		t.Fatalf("yaml: %v\n%s", err, cloudConfig)
	}
	return m
}

func toStrings(t *testing.T, v any) []string {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("want []any, got %T", v)
	}
	out := make([]string, len(list))
	for i, x := range list {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("elem %d: %T", i, x)
		}
		out[i] = s
	}
	return out
}

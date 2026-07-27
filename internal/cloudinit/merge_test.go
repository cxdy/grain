package cloudinit

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestMergeUserData_ShellExtraAsRuncmd(t *testing.T) {
	base := `#cloud-config
hostname: sbox-1
runcmd:
  - [ sh, -c, "echo grain-ready > /var/lib/grain-ready" ]
`
	got, err := MergeUserData(base, "apt-get update && apt-get install -y curl")
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
	got, err := MergeUserData(base, extra)
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
	got, err := MergeUserData(base, "")
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
	got, err := MergeUserData(base, extra)
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
	got, err := MergeUserData(base, extra)
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
	got, err := MergeUserData(base, script)
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
	got, err := RenderUserData("sbox-9", "ssh-ed25519 AAAA test@grain", "echo hi")
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
	got, err := RenderUserData("sbox-1", "ssh-ed25519 AAAA k", "",
		MountSpec{Tag: "grain0", Guest: "/mnt/src"},
		MountSpec{Tag: "grain1", Guest: "/data"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want0 := MountRuncmd("grain0", "/mnt/src", "9p")
	want1 := MountRuncmd("grain1", "/data", "")
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
	got, err := RenderUserDataMinimal("clone-1", key, "")
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
	full, err := RenderUserData("sbox-9", key, "")
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
	got, err := RenderUserDataMinimal("m", "ssh-ed25519 AAAA k", "echo extra",
		MountSpec{Tag: "grain0", Guest: "/work"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, MountRuncmd("grain0", "/work", "9p")) {
		t.Fatalf("missing mount runcmd:\n%s", got)
	}
	if !strings.Contains(got, "echo extra") {
		t.Fatalf("missing shell extra:\n%s", got)
	}
}

func TestBaseUserDataMinimal_Map(t *testing.T) {
	m := BaseUserDataMinimal("h1", "ssh-ed25519 BBBB k")
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
	got := MountRuncmd("grain0", "/work", "9p")
	want := "mkdir -p /work && mount -t 9p -o trans=virtio,version=9p2000.L grain0 /work"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// empty driver defaults to 9p
	if got := MountRuncmd("grain0", "/work", ""); got != want {
		t.Fatalf("empty driver: got %q want %q", got, want)
	}
	vfs := MountRuncmd("grain0", "/work", "virtiofs")
	wantVFS := "mkdir -p /work && mount -t virtiofs grain0 /work"
	if vfs != wantVFS {
		t.Fatalf("virtiofs: got %q want %q", vfs, wantVFS)
	}
}

func TestRenderUserData_MountRuncmdsVirtiofs(t *testing.T) {
	got, err := RenderUserData("sbox-1", "ssh-ed25519 AAAA k", "",
		MountSpec{Tag: "grain0", Guest: "/mnt/src", Driver: "virtiofs"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := MountRuncmd("grain0", "/mnt/src", "virtiofs")
	if !strings.Contains(got, want) {
		t.Fatalf("missing virtiofs mount %q in:\n%s", want, got)
	}
	if strings.Contains(got, "mount -t 9p") {
		t.Fatalf("should not inject 9p mount for virtiofs driver:\n%s", got)
	}
}

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

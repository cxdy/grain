package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckConfigContentOK(t *testing.T) {
	// Force in-process validate (no grain needed)
	r := &fakeRunner{lookErr: os.ErrNotExist}
	tmp, err := CheckConfigContent("hypervisor: qemu\napi: 127.0.0.1:7474\n", r)
	if tmp != "" {
		defer os.Remove(tmp)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckConfigContentBad(t *testing.T) {
	r := &fakeRunner{lookErr: os.ErrNotExist}
	tmp, err := CheckConfigContent("hypervisor: xen\n", r)
	if tmp != "" {
		defer os.Remove(tmp)
	}
	if err == nil {
		t.Fatal("want error")
	}
}

func TestCheckConfigContentUnknownKey(t *testing.T) {
	r := &fakeRunner{lookErr: os.ErrNotExist}
	tmp, err := CheckConfigContent("urmom:\n  is: True\nhypervisor: qemu\napi: 127.0.0.1:7474\n", r)
	if tmp != "" {
		defer os.Remove(tmp)
	}
	if err == nil {
		t.Fatal("want unknown key error")
	}
	if !strings.Contains(err.Error(), "urmom") && !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("want unknown key mention, got %v", err)
	}
}

func TestGenerateAndRevokeAPIToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: qemu\napi: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := GenerateAPIToken(path)
	if err != nil || res.Token == "" || !res.HasToken {
		t.Fatalf("%+v %v", res, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "api_token:") {
		t.Fatalf("%s", b)
	}
	res2, err := RevokeAPIToken(path)
	if err != nil || res2.HasToken {
		t.Fatalf("%+v %v", res2, err)
	}
	b2, _ := os.ReadFile(path)
	if strings.Contains(string(b2), "api_token:") {
		t.Fatalf("token still present: %s", b2)
	}
}

func TestSaveConfigTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	r := &fakeRunner{lookErr: os.ErrNotExist}
	_, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474", false, r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		t.Fatalf("want trailing newline, got %q", b)
	}
}

func TestSaveConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	r := &fakeRunner{lookErr: os.ErrNotExist, path: ""}
	// LookPath fails → in-process validate; no restart
	res, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n", false, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != path {
		t.Fatalf("%+v", res)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "hypervisor: qemu") {
		t.Fatalf("%s %v", b, err)
	}
}

func TestSaveConfigFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	r := &fakeRunner{lookErr: os.ErrNotExist}
	_, err := SaveConfigFile(path, "hypervisor: bad\n", false, r)
	if err == nil || !strings.Contains(err.Error(), "check-config") {
		t.Fatalf("%v", err)
	}
}

func TestReadConfigFileMissing(t *testing.T) {
	s, err := ReadConfigFile(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil || !strings.Contains(s, "grain config") {
		t.Fatalf("%q %v", s, err)
	}
}

// ensure CommandRunner interface still used
var _ CommandRunner = ExecRunner{}

func TestSaveConfigRestartMessageWithoutGrain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	r := &fakeRunner{lookErr: os.ErrNotExist}
	res, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474\n", true, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.DaemonRestarted {
		t.Fatal("should not restart without grain")
	}
	if !strings.Contains(res.Message, "PATH") {
		t.Fatalf("%+v", res)
	}
}

func TestSaveConfigRestartWithGrain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Use real grain for check-config if available; fake runner for restart.
	grainPath, err := (ExecRunner{}).LookPath("grain")
	if err != nil {
		t.Skip("grain not on PATH")
	}
	// Real check via grain, then fake start for down/up
	r := &restartRunner{grain: grainPath}
	res, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:17474\n", true, r)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DaemonRestarted || r.starts < 2 {
		t.Fatalf("res=%+v starts=%d", res, r.starts)
	}
}

type restartRunner struct {
	grain  string
	starts int
}

func (r *restartRunner) LookPath(file string) (string, error) {
	if file == "grain" {
		return r.grain, nil
	}
	return "", os.ErrNotExist
}

func (r *restartRunner) StartBackground(ctx context.Context, name string, args ...string) error {
	r.starts++
	return nil
}

func mustLookGrain(t *testing.T) string {
	t.Helper()
	p, err := ExecRunner{}.LookPath("grain")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadConfigFileOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("cpus: 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ReadConfigFile(path)
	if err != nil || !strings.Contains(s, "cpus: 4") {
		t.Fatalf("%q %v", s, err)
	}
}

func TestCheckConfigContentViaGrainCLI(t *testing.T) {
	if _, err := (ExecRunner{}).LookPath("grain"); err != nil {
		t.Skip("no grain")
	}
	tmp, err := CheckConfigContent("hypervisor: qemu\napi: 127.0.0.1:7474\n", ExecRunner{})
	if tmp != "" {
		defer os.Remove(tmp)
	}
	if err != nil {
		t.Fatal(err)
	}
	tmp2, err := CheckConfigContent("hypervisor: xen\n", ExecRunner{})
	if tmp2 != "" {
		defer os.Remove(tmp2)
	}
	if err == nil {
		t.Fatal("want fail")
	}
}

func TestSaveConfigFileEmptyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// force no grain so we don't restart real daemon
	r := &fakeRunner{lookErr: os.ErrNotExist}
	res, err := SaveConfigFile("", "hypervisor: qemu\napi: 127.0.0.1:7474\n", false, r)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".grain", "config.yaml")
	if res.Path != want {
		t.Fatalf("path %q want %q", res.Path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal(err)
	}
}

func TestReadConfigFileEmptyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// missing → starter template
	s, err := ReadConfigFile("")
	if err != nil || !strings.Contains(s, "grain config") {
		t.Fatalf("%q %v", s, err)
	}
}

func TestParsePublishMounts(t *testing.T) {
	t.Parallel()
	fw, err := parsePublish("8080:80, 443:443")
	if err != nil || len(fw) != 2 {
		t.Fatalf("%+v %v", fw, err)
	}
	if _, err := parsePublish("bad"); err == nil {
		t.Fatal("want err")
	}
	mts, err := parseMounts("/a:/b\n/c:/d")
	if err != nil || len(mts) != 2 {
		t.Fatalf("%+v %v", mts, err)
	}
	if _, err := parseMounts("nocolon"); err == nil {
		t.Fatal("want err")
	}
}

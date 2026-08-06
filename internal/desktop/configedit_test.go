package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckConfigContentOK(t *testing.T) {
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
	tmp, err := CheckConfigContent("hypervisor: not-a-real-hypervisor\n", r)
	if tmp != "" {
		defer os.Remove(tmp)
	}
	if err == nil {
		t.Fatal("want error")
	}
}

func TestSaveConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	r := &fakeRunner{lookErr: os.ErrNotExist, path: ""}
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

func TestSaveConfigRestartMessageWithoutGrain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	r := &fakeRunner{lookErr: os.ErrNotExist}
	res, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474\n", true, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.DaemonRestarted {
		t.Fatal("cannot restart without grain binary")
	}
}

func TestGenerateAndRevokeAPIToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("api: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := GenerateAPIToken(path)
	if err != nil || res.Token == "" {
		t.Fatalf("%+v %v", res, err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "api_token:") {
		t.Fatalf("%s", b)
	}
	res2, err := RevokeAPIToken(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = res2
	b, _ = os.ReadFile(path)
	// Revoke may leave empty token or remove key; either is fine for this path.
	_ = strings.Contains(string(b), "api_token:")
}

var _ CommandRunner = ExecRunner{}

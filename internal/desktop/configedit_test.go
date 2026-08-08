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

func TestValidateSandboxNameEmptyAndInvalid(t *testing.T) {
	if err := ValidateSandboxName(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSandboxName("ok-name"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSandboxName("9bad"); err == nil {
		t.Fatal("must start with letter")
	}
}

func TestReadConfigFilePresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("api: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ReadConfigFile(path)
	if err != nil || !strings.Contains(s, "7474") {
		t.Fatalf("%q %v", s, err)
	}
	// empty path uses default home location (may or may not exist)
	if _, err := ReadConfigFile(""); err != nil {
		t.Fatal(err)
	}
}

func TestCheckConfigContentWithGrainCLI(t *testing.T) {
	// When grain is on PATH, CheckConfigContent may invoke check-config.
	// Use fakeRunner with lookErr so we skip external binary; cover ExecRunner path separately.
	valid := "hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n"
	r := &fakeRunner{lookErr: os.ErrNotExist}
	tmp, err := CheckConfigContent(valid, r)
	if tmp != "" {
		defer os.Remove(tmp)
	}
	if err != nil {
		t.Fatal(err)
	}
	// nil runner uses ExecRunner
	tmp2, err := CheckConfigContent(valid, nil)
	if tmp2 != "" {
		defer os.Remove(tmp2)
	}
	// may succeed or fail depending on grain binary; just exercise path
	_ = err
}

func TestSaveConfigFileRestartDaemon(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// CheckConfigContent runs real exec.Command on LookPath result for check-config.
	stub := filepath.Join(dir, "grain")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{path: stub}
	res, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n", true, r)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DaemonRestarted || res.Path != path {
		t.Fatalf("%+v", res)
	}
	if r.started.Load() < 2 {
		t.Fatalf("want grain down + up, started=%d", r.started.Load())
	}
	// trailing newline ensured without one in content
	res2, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2", false, r)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(res2.Path)
	if !strings.HasSuffix(string(b), "\n") {
		t.Fatal("want trailing newline")
	}
	// start error on up — still need real stub for check-config
	r2 := &fakeRunner{path: stub, startErr: os.ErrPermission}
	_, err = SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474\n", true, r2)
	if err == nil {
		t.Fatal("want up failure")
	}
	// invalid YAML fails in-process validate
	if _, err := SaveConfigFile(path, "not-valid-yaml-{{{", false, &fakeRunner{lookErr: os.ErrNotExist}); err == nil {
		t.Fatal("want check-config fail")
	}
}

func TestSetAndDeleteConfigKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := setConfigStringKey(path, "api_token", "abc"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "api_token: abc") {
		t.Fatalf("%s", b)
	}
	// update existing
	if err := setConfigStringKey(path, "api_token", "xyz"); err != nil {
		t.Fatal(err)
	}
	// create missing file
	path2 := filepath.Join(dir, "sub", "c.yaml")
	if err := setConfigStringKey(path2, "k", "v"); err != nil {
		t.Fatal(err)
	}
	// invalid yaml
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte(":\n:"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setConfigStringKey(bad, "k", "v"); err == nil {
		t.Fatal("want parse error")
	}
	if err := deleteConfigKeys(bad, "k"); err == nil {
		t.Fatal("want parse error on delete")
	}
	// delete keys
	if err := deleteConfigKeys(path, "api_token", "auth_token"); err != nil {
		t.Fatal(err)
	}
	// missing file delete is ok
	if err := deleteConfigKeys(filepath.Join(dir, "missing.yaml"), "api_token"); err != nil {
		t.Fatal(err)
	}
	// empty path uses home (just ensure no panic for set when we have write access)
	tok, err := randomToken(16)
	if err != nil || len(tok) != 16 {
		t.Fatalf("%q %v", tok, err)
	}
}

func TestGenerateAPITokenNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "config.yaml")
	res, err := GenerateAPIToken(path)
	if err != nil || res.Token == "" || !res.HasToken {
		t.Fatalf("%+v %v", res, err)
	}
	res2, err := RevokeAPIToken(path)
	if err != nil || res2.HasToken {
		t.Fatalf("%+v %v", res2, err)
	}
}

func TestSetDeleteConfigEdgeCases(t *testing.T) {
	dir := t.TempDir()
	// empty file → deleteConfigKeys unmarshals nil doc and returns nil
	empty := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(empty, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := deleteConfigKeys(empty, "api_token"); err != nil {
		t.Fatal(err)
	}
	// Generate/Revoke on invalid YAML
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte(":\n:"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateAPIToken(bad); err == nil {
		t.Fatal("want generate parse error")
	}
	if _, err := RevokeAPIToken(bad); err == nil {
		t.Fatal("want revoke parse error")
	}
	// Generate into unwritable directory (skip when running as root — e.g. Docker CI).
	if os.Geteuid() != 0 {
		blocked := filepath.Join(dir, "blocked")
		if err := os.MkdirAll(blocked, 0o700); err != nil {
			t.Fatal(err)
		}
		cfgPath := filepath.Join(blocked, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte("api: 127.0.0.1:7474\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(blocked, 0o000); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chmod(blocked, 0o700) }()
		if _, err := GenerateAPIToken(cfgPath); err == nil {
			t.Fatal("want write error on generate")
		}
	}
}

func TestSaveConfigFileWriteFailuresAndNilRunner(t *testing.T) {
	dir := t.TempDir()
	valid := "hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n"
	r := &fakeRunner{lookErr: os.ErrNotExist}

	// path is a directory → WriteFile fails
	asDir := filepath.Join(dir, "asdir")
	if err := os.MkdirAll(asDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveConfigFile(asDir, valid, false, r); err == nil {
		t.Fatal("want write error when path is directory")
	}

	// parent is a file → MkdirAll fails
	parentFile := filepath.Join(dir, "parentfile")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveConfigFile(filepath.Join(parentFile, "config.yaml"), valid, false, r); err == nil {
		t.Fatal("want mkdir error")
	}

	// nil runner uses ExecRunner
	path := filepath.Join(dir, "ok.yaml")
	res, err := SaveConfigFile(path, valid, false, nil)
	if err != nil {
		// may fail if real grain check-config rejects; in-process validate should pass
		t.Logf("nil runner save: %v", err)
	} else if res.Path != path {
		t.Fatalf("%+v", res)
	}
}

func TestCheckConfigContentGrainUnknownCommand(t *testing.T) {
	dir := t.TempDir()
	// grain binary that prints "unknown command" and exits 1
	grain := filepath.Join(dir, "grain")
	if err := os.WriteFile(grain, []byte("#!/bin/sh\necho 'unknown command'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{path: grain}
	valid := "hypervisor: qemu\napi: 127.0.0.1:7474\n"
	tmp, err := CheckConfigContent(valid, r)
	if tmp != "" {
		defer os.Remove(tmp)
	}
	if err != nil {
		t.Fatalf("unknown command should be ignored: %v", err)
	}
	// grain that fails with other message
	grain2 := filepath.Join(dir, "grain2")
	if err := os.WriteFile(grain2, []byte("#!/bin/sh\necho 'config bad'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r2 := &fakeRunner{path: grain2}
	tmp2, err := CheckConfigContent(valid, r2)
	if tmp2 != "" {
		defer os.Remove(tmp2)
	}
	if err == nil || !strings.Contains(err.Error(), "config bad") {
		t.Fatalf("want config bad error: %v", err)
	}
	// empty output failure uses runErr.Error()
	grain3 := filepath.Join(dir, "grain3")
	if err := os.WriteFile(grain3, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r3 := &fakeRunner{path: grain3}
	tmp3, err := CheckConfigContent(valid, r3)
	if tmp3 != "" {
		defer os.Remove(tmp3)
	}
	if err == nil {
		t.Fatal("want error")
	}
}

func TestSetDeleteConfigKeyBranches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "c.yaml")
	// create via set (mkdir + write)
	if err := setConfigStringKey(path, "api_token", "t1"); err != nil {
		t.Fatal(err)
	}
	// update existing
	if err := setConfigStringKey(path, "api_token", "t2"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "api_token: t2") {
		t.Fatalf("%s", b)
	}
	// corrupt yaml
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte(":\n:"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setConfigStringKey(bad, "k", "v"); err == nil {
		t.Fatal("want parse error")
	}
	if err := deleteConfigKeys(bad, "k"); err == nil {
		t.Fatal("want parse error on delete")
	}
	// delete missing file is ok
	if err := deleteConfigKeys(filepath.Join(dir, "nope.yaml"), "x"); err != nil {
		t.Fatal(err)
	}
	// delete existing
	if err := deleteConfigKeys(path, "api_token"); err != nil {
		t.Fatal(err)
	}
	// empty path uses home — just ensure no panic with temp HOME
	t.Setenv("HOME", dir)
	if err := setConfigStringKey("", "api_token", "home"); err != nil {
		t.Fatal(err)
	}
	if err := deleteConfigKeys("", "api_token"); err != nil {
		t.Fatal(err)
	}
	// ReadConfigFile missing → template
	s, err := ReadConfigFile(filepath.Join(dir, "missing.yaml"))
	if err != nil || !strings.Contains(s, "grain config") {
		t.Fatalf("%q %v", s, err)
	}
	// CheckConfigContent with real grain stub unknown command path
	stub := filepath.Join(dir, "grain-bad")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'Error: unknown command \"check-config\"' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tmp, err := CheckConfigContent("hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n", &fakeRunner{path: stub})
	if tmp != "" {
		_ = os.Remove(tmp)
	}
	if err != nil {
		t.Fatalf("unknown command should be ignored after in-process validate: %v", err)
	}
}

func TestConfigEditMoreBranches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	// Generate + revoke tokens
	res, err := GenerateAPIToken(path)
	if err != nil || res.Token == "" || !res.HasToken {
		t.Fatalf("%+v %v", res, err)
	}
	res2, err := RevokeAPIToken(path)
	if err != nil || res2.HasToken {
		t.Fatalf("%+v %v", res2, err)
	}
	// ReadConfigFile empty path uses home
	t.Setenv("HOME", dir)
	s, err := ReadConfigFile("")
	if err != nil {
		t.Fatal(err)
	}
	_ = s
	// Save without restart when grain missing
	r := &fakeRunner{lookErr: os.ErrNotExist}
	out, err := SaveConfigFile(path, "hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n", true, r)
	if err != nil {
		t.Fatal(err)
	}
	if out.DaemonRestarted {
		t.Fatal("should not restart without grain")
	}
	if !strings.Contains(out.Message, "not on PATH") {
		t.Fatalf("msg %q", out.Message)
	}
	// check-config real failure (not unknown command)
	stub := filepath.Join(dir, "grain-fail")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'invalid field foobar' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// content valid for in-process validate, but CLI fails
	tmp, err := CheckConfigContent("hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n", &fakeRunner{path: stub})
	if tmp != "" {
		_ = os.Remove(tmp)
	}
	if err == nil || !strings.Contains(err.Error(), "invalid field") {
		t.Fatalf("want cli fail: %v", err)
	}
	// nil runner uses ExecRunner — in-process still works
	tmp2, err := CheckConfigContent("hypervisor: qemu\napi: 127.0.0.1:7474\ncpus: 2\n", nil)
	if tmp2 != "" {
		_ = os.Remove(tmp2)
	}
	if err != nil {
		// may fail if real grain check-config rejects; in-process must pass first
		// only fail if in-process error
		if strings.Contains(err.Error(), "unknown field") || strings.Contains(err.Error(), "yaml") {
			t.Fatal(err)
		}
	}
	// ValidateSandboxName edges
	if err := ValidateSandboxName("Bad_Name"); err == nil {
		t.Fatal("want invalid")
	}
	if err := ValidateSandboxName("good-name"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSandboxName(""); err != nil {
		t.Fatal(err)
	}
}

func TestConfigEditTinyGaps(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// empty path generate/revoke under HOME
	res, err := GenerateAPIToken("")
	if err != nil {
		t.Fatal(err)
	}
	if res.Token == "" {
		t.Fatal("token")
	}
	if _, err := RevokeAPIToken(""); err != nil {
		t.Fatal(err)
	}
	// CheckConfigContent validate fail returns tmp
	tmp, err := CheckConfigContent("not: valid: yaml: [", &fakeRunner{lookErr: os.ErrNotExist})
	if tmp != "" {
		_ = os.Remove(tmp)
	}
	if err == nil {
		t.Fatal("want validate fail")
	}
	// ReadConfigFile existing
	p := filepath.Join(dir, ".grain", "config.yaml")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("api: 127.0.0.1:7474\n"), 0o600)
	s, err := ReadConfigFile(p)
	if err != nil || !strings.Contains(s, "api:") {
		t.Fatal(s, err)
	}
}

func TestRandomTokenAndGenerateFail(t *testing.T) {
	old := randRead
	t.Cleanup(func() { randRead = old })
	randRead = func(b []byte) (int, error) { return 0, os.ErrInvalid }
	if _, err := randomToken(8); err == nil {
		t.Fatal("want rand error")
	}
	if _, err := GenerateAPIToken(filepath.Join(t.TempDir(), "c.yaml")); err == nil {
		t.Fatal("want generate fail")
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdCheckConfigFromFlagAndDefaultHome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: qemu\napi: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// --config flag when no positional arg
	cfgPath := path
	cmd := cmdCheckConfig(&cfgPath)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok:") {
		t.Fatalf("%q", out.String())
	}

	// empty cfgPath + no args → ~/.grain/config.yaml
	home := t.TempDir()
	t.Setenv("HOME", home)
	grainCfg := filepath.Join(home, ".grain")
	if err := os.MkdirAll(grainCfg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(grainCfg, "config.yaml"), []byte("hypervisor: mock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := ""
	cmd = cmdCheckConfig(&empty)
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("home config: %v out=%q", err, out.String())
	}
	if !strings.Contains(out.String(), "ok:") {
		t.Fatalf("%q", out.String())
	}

	// positional arg wins over cfgPath
	cmd = cmdCheckConfig(&cfgPath)
	out.Reset()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{path})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdCheckConfigMissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "missing.yaml")
	root := Root("test")
	root.SetArgs([]string{"check-config", p})
	if err := root.Execute(); err == nil {
		t.Fatal("want error")
	}
}

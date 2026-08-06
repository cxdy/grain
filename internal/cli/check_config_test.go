package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdCheckConfigOK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: qemu\napi: 127.0.0.1:7474\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := Root("test")
	root.SetArgs([]string{"check-config", path})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok:") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestCmdCheckConfigBad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(path, []byte("hypervisor: xen\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := Root("test")
	root.SetArgs([]string{"check-config", path})
	if err := root.Execute(); err == nil {
		t.Fatal("want error")
	}
}

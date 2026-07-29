package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdRecipeValidateAndShow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.recipe.yaml")
	body := `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
spec:
  image: grain-ubuntu
  bootstrap:
    steps:
      - name: ok
        run: true
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	root := Root("test")
	root.SetArgs([]string{"recipe", "validate", path})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ok") || !strings.Contains(out.String(), "bootstrap_steps=1") {
		t.Fatalf("validate out: %q", out.String())
	}

	root = Root("test")
	root.SetArgs([]string{"recipe", "show", path, "--userdata"})
	out.Reset()
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "wait:") || !strings.Contains(s, "bootstrap") {
		t.Fatalf("show out: %q", s)
	}
	if !strings.Contains(s, "grain_ready_report") {
		t.Fatalf("expected userdata in show: %q", s)
	}
}

func TestCmdNewRecipeFlagRegistered(t *testing.T) {
	cfg := t.TempDir()
	cmd := cmdNew(&cfg)
	if cmd.Flags().Lookup("recipe") == nil {
		t.Fatal("missing --recipe flag")
	}
}

func TestCmdRecipeValidateBad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("kind: Sandbox\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := Root("test")
	root.SetArgs([]string{"recipe", "validate", path})
	if err := root.Execute(); err == nil {
		t.Fatal("expected validate error")
	}
}

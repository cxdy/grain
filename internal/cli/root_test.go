package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	cmd := Root("test-version")
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"grain", "up", "new", "ls", "proxy", "secret"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestRootSubcommandsPresent(t *testing.T) {
	cmd := Root("0.0.0-test")
	want := []string{
		"up", "down", "new", "act", "stop", "start", "pause", "resume",
		"suspend", "restore", "ls", "rm", "sh", "x", "cp", "fs", "logs",
		"fwd", "stats", "secret", "proxy", "profile", "image", "agent",
		"doctor", "tray", "version",
	}
	for _, name := range want {
		found := false
		for _, c := range cmd.Commands() {
			if c.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing subcommand %q", name)
		}
	}
}

func TestCmdVersion(t *testing.T) {
	// cmdVersion uses fmt.Println (os.Stdout), not cmd.Out.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	cmd := Root("v1.2.3-test")
	cmd.SetArgs([]string{"version"})
	err = cmd.Execute()
	_ = w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(r)
	_ = r.Close()
	if !strings.Contains(string(out), "v1.2.3-test") {
		t.Fatalf("version output: %q", out)
	}
}

func TestRootPersistentFlags(t *testing.T) {
	cmd := Root("t")
	if cmd.PersistentFlags().Lookup("config") == nil {
		t.Fatal("missing --config")
	}
	if cmd.PersistentFlags().Lookup("api") == nil {
		t.Fatal("missing --api")
	}
}

func TestCmdNewFlags(t *testing.T) {
	cfg := ""
	cmd := cmdNew(&cfg)
	for _, name := range []string{"persist", "name", "cpus", "mem", "disk", "image", "arch", "gpu", "network", "userdata-file", "profile", "preset", "wait", "publish", "volume", "publish-socket", "proxy"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

func TestCmdCpFlags(t *testing.T) {
	cfg := ""
	cmd := cmdCp(&cfg)
	if cmd.Flags().Lookup("ssh") == nil || cmd.Flags().Lookup("agent") == nil {
		t.Fatal("missing ssh/agent flags")
	}
	cmd.SetArgs([]string{"--ssh", "--agent", "a", "b"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot use --ssh and --agent") {
		t.Fatalf("want mutual exclusion error, got %v", err)
	}
}

func TestCmdXSSHAndAgentMutuallyExclusive(t *testing.T) {
	cfg := ""
	cmd := cmdX(&cfg)
	cmd.SetArgs([]string{"--ssh", "--agent", "--", "true"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot use --ssh and --agent") {
		t.Fatalf("want mutual exclusion error, got %v", err)
	}
}

func TestCmdActFlags(t *testing.T) {
	cfg := ""
	cmd := cmdAct(&cfg)
	for _, name := range []string{"keep", "dir", "name", "cpus", "mem", "image", "timeout"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s", name)
		}
	}
}

func TestCmdLogsFlags(t *testing.T) {
	cfg := ""
	cmd := cmdLogs(&cfg)
	if cmd.Flags().Lookup("follow") == nil || cmd.Flags().Lookup("qemu") == nil {
		t.Fatal("missing follow/qemu flags")
	}
}

func TestCmdFsSubcommands(t *testing.T) {
	cfg := ""
	root := cmdFs(&cfg)
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"ls", "stat", "mkdir", "rm"} {
		if !names[want] {
			t.Errorf("fs missing %s", want)
		}
	}
}

func TestCmdSecretSubcommands(t *testing.T) {
	cfg := ""
	root := cmdSecret(&cfg)
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"ls", "set", "rm", "inject"} {
		if !names[want] {
			t.Errorf("secret missing %s", want)
		}
	}
}

func TestCmdProxySubcommands(t *testing.T) {
	cfg := ""
	root := cmdProxy(&cfg)
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"up", "down", "allow", "deny", "ls", "client"} {
		if !names[want] {
			t.Errorf("proxy missing %s", want)
		}
	}
}

func TestCmdImageSubcommands(t *testing.T) {
	cfg := ""
	root := cmdImage(&cfg)
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"ls", "pull", "import"} {
		if !names[want] {
			t.Errorf("image missing %s", want)
		}
	}
}

func TestCmdFwdSubcommands(t *testing.T) {
	cfg := ""
	root := cmdFwd(&cfg)
	names := map[string]bool{}
	for _, c := range root.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"ls", "add", "rm"} {
		if !names[want] {
			t.Errorf("fwd missing %s", want)
		}
	}
}

func TestCmdAgentSubcommands(t *testing.T) {
	cfg := ""
	root := cmdAgent(&cfg)
	if len(root.Commands()) == 0 {
		t.Fatal("agent has no subcommands")
	}
	if root.Commands()[0].Name() != "health" {
		t.Fatalf("want health, got %s", root.Commands()[0].Name())
	}
}

func TestCmdProfileSubcommands(t *testing.T) {
	cfg := ""
	root := cmdProfile(&cfg)
	if len(root.Commands()) != 1 || root.Commands()[0].Name() != "ls" {
		t.Fatalf("profile cmds: %v", root.Commands())
	}
}

func TestCmdTrayWindowsUnsupported(t *testing.T) {
	// Construction only — RunE on non-windows still needs tray lib.
	cfg := ""
	cmd := cmdTray(&cfg, "v")
	if cmd.Use != "tray" {
		t.Fatalf("Use=%q", cmd.Use)
	}
}

package cli

import (
	"strings"
	"testing"
)

func TestCmdShFlags(t *testing.T) {
	cfg := ""
	cmd := cmdSh(&cfg)
	if cmd.Use != "sh [name]" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	ssh := cmd.Flags().Lookup("ssh")
	if ssh == nil {
		t.Fatal("missing --ssh flag")
		return
	}
	agent := cmd.Flags().Lookup("agent")
	if agent == nil {
		t.Fatal("missing --agent flag")
		return
	}
	// Defaults false.
	if ssh.DefValue != "false" || agent.DefValue != "false" {
		t.Fatalf("flag defaults: ssh=%s agent=%s", ssh.DefValue, agent.DefValue)
	}
}

func TestCmdShSSHAndAgentMutuallyExclusive(t *testing.T) {
	cfg := ""
	cmd := cmdSh(&cfg)
	cmd.SetArgs([]string{"--ssh", "--agent"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --ssh and --agent together")
	}
	if !strings.Contains(err.Error(), "cannot use --ssh and --agent") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsAgentUnavailableForShell(t *testing.T) {
	if !isAgentUnavailable(errAgentSkip) {
		t.Fatal("errAgentSkip should be unavailable")
	}
	if isAgentUnavailable(nil) {
		t.Fatal("nil should not be unavailable")
	}
}

package cli

import (
	"path/filepath"
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
	fwd := cmd.Flags().Lookup("A")
	if fwd == nil {
		fwd = cmd.Flags().ShorthandLookup("A")
	}
	if fwd == nil {
		t.Fatal("missing -A / --forward-client flag")
	}
	if !strings.Contains(fwd.Usage, "SSH agent") || !strings.Contains(strings.ToLower(fwd.Usage), "socks") {
		t.Fatalf("-A usage should describe session client forwarding, got %q", fwd.Usage)
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

func TestCmdShAAndSSHMutuallyExclusive(t *testing.T) {
	cfg := ""
	cmd := cmdSh(&cfg)
	cmd.SetArgs([]string{"-A", "--ssh"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for -A and --ssh together")
	}
	if !strings.Contains(err.Error(), "-A") || !strings.Contains(err.Error(), "--ssh") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCmdShAMissingAuthSockRefusesPTY(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	cfg := ""
	cmd := cmdSh(&cfg)
	cmd.SetArgs([]string{"-A"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when SSH_AUTH_SOCK is empty")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ssh_auth_sock") {
		t.Fatalf("error should mention SSH_AUTH_SOCK: %v", err)
	}
}

func TestCmdShAUnusableAuthSockRefusesPTY(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing.sock"))
	cfg := ""
	cmd := cmdSh(&cfg)
	cmd.SetArgs([]string{"-A", "any-name"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when SSH_AUTH_SOCK is not a socket")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ssh_auth_sock") {
		t.Fatalf("error should mention SSH_AUTH_SOCK: %v", err)
	}
}

func TestCmdShWithoutADoesNotRequireAuthSock(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	cfg := ""
	cmd := cmdSh(&cfg)
	if cmd.Flags().ShorthandLookup("A") == nil && cmd.Flags().Lookup("forward-client") == nil {
		t.Fatal("missing -A")
	}
	// Default sh must not inspect SSH_AUTH_SOCK; flag default is false.
	fwd, _ := cmd.Flags().GetBool("forward-client")
	if fwd {
		t.Fatal("default sh must not enable -A")
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

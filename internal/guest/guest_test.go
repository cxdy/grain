package guest

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEnsureAgentValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	if err := EnsureAgent(ctx, "", 22, "u", "", "/bin/true"); err == nil || !strings.Contains(err.Error(), "empty host") {
		t.Fatalf("empty host: %v", err)
	}
	if err := EnsureAgent(ctx, "127.0.0.1", 22, "", "", "/bin/true"); err == nil || !strings.Contains(err.Error(), "empty user") {
		t.Fatalf("empty user: %v", err)
	}
	if err := EnsureAgent(ctx, "127.0.0.1", 22, "u", "", ""); err == nil || !strings.Contains(err.Error(), "empty binary") {
		t.Fatalf("empty binary: %v", err)
	}
	if err := EnsureAgent(ctx, "127.0.0.1", 22, "u", "", filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "binary") {
		t.Fatalf("missing binary: %v", err)
	}
	dir := t.TempDir()
	if err := EnsureAgent(ctx, "127.0.0.1", 22, "u", "", dir); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("dir as binary: %v", err)
	}
}

func TestAgentServiceUnitContent(t *testing.T) {
	t.Parallel()
	if !strings.Contains(AgentServiceUnit, "grain-agent") {
		t.Fatal("unit missing grain-agent")
	}
	if !strings.Contains(AgentServiceUnit, ":7475") {
		t.Fatal("unit missing listen port")
	}
	if GuestAgentBin != "/usr/local/bin/grain-agent" {
		t.Fatalf("GuestAgentBin %q", GuestAgentBin)
	}
	if GuestAgentServicePath != "/etc/systemd/system/grain-agent.service" {
		t.Fatalf("GuestAgentServicePath %q", GuestAgentServicePath)
	}
}

func TestSSHArgsPortZeroAndEmptyIdentity(t *testing.T) {
	t.Parallel()
	args := SSHArgs("root", "h", 0, "")
	for _, a := range args {
		if a == "-p" || a == "-i" {
			t.Fatalf("port 0 / empty id should omit flags: %v", args)
		}
	}
	if args[len(args)-1] != "root@h" {
		t.Fatalf("user@host: %v", args)
	}

	scp := SCPArgs(0, "")
	for _, a := range scp {
		if a == "-P" || a == "-i" {
			t.Fatalf("SCPArgs omit: %v", scp)
		}
	}

	probe := SSHProbeArgs("u", "h", 0, "")
	if !containsPair(probe, "-o", "BatchMode=yes") {
		t.Fatalf("probe: %v", probe)
	}
	for i, a := range probe {
		if a == "-p" {
			t.Fatalf("port 0 should omit -p: %v", probe)
		}
		_ = i
	}
}

func TestWaitSSHAcceptsTCPWithoutKey(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	// Accept and close connections so Dial succeeds.
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Empty privKey → WaitSSH returns after successful TCP dial.
	if err := WaitSSH(ctx, "127.0.0.1", port, "ubuntu", ""); err != nil {
		t.Fatalf("WaitSSH: %v", err)
	}
}

func TestWaitSSHTimeout(t *testing.T) {
	t.Parallel()
	// Nothing listening on a closed port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = WaitSSH(ctx, "127.0.0.1", port, "ubuntu", "")
	if err == nil {
		t.Fatal("expected timeout/cancel")
	}
}

func TestWaitSSHDefaultDeadline(t *testing.T) {
	t.Parallel()
	// Context without deadline uses internal 90s timeout; cancel quickly.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	// Use port that won't accept.
	err := WaitSSH(ctx, "127.0.0.1", 1, "ubuntu", "")
	if err == nil {
		t.Fatal("expected error after cancel")
	}
}

func TestEnsureAgentFailsOnSCP(t *testing.T) {
	// Not parallel: may race with PATH-mutating tests that fake scp/ssh.
	// Real binary path but SSH/SCP will fail (nothing listening / bad key).
	bin := filepath.Join(t.TempDir(), "grain-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := EnsureAgent(ctx, "127.0.0.1", 1, "ubuntu", filepath.Join(t.TempDir(), "nokey"), bin)
	if err == nil {
		t.Fatal("expected scp/install failure")
	}
	if !strings.Contains(err.Error(), "ensure agent") {
		t.Fatalf("error %v", err)
	}
}

func TestWaitSSHWithKeyProbesAndFails(t *testing.T) {
	// Not parallel: invokes real ssh; avoid racing PATH fakes.
	// TCP accepts but ssh probe fails (not an SSH server).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	// Dummy key file path — ssh will fail BatchMode.
	key := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(key, []byte("not-a-real-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	err = WaitSSH(ctx, "127.0.0.1", port, "ubuntu", key)
	if err == nil {
		t.Fatal("expected wait ssh failure with bogus key")
	}
}

func TestEnsureAgentSuccessWithFakeSSH(t *testing.T) {
	// Not parallel: mutates PATH for this process.
	binDir := t.TempDir()
	// Fake scp/ssh that always succeed.
	for _, name := range []string{"scp", "ssh"} {
		path := filepath.Join(binDir, name)
		script := "#!/bin/sh\nexit 0\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	agentBin := filepath.Join(t.TempDir(), "grain-agent")
	if err := os.WriteFile(agentBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := EnsureAgent(ctx, "127.0.0.1", 2222, "ubuntu", "", agentBin); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
}

func TestEnsureAgentInstallSSHFails(t *testing.T) {
	// Not parallel: mutates PATH.
	binDir := t.TempDir()
	// scp succeeds; ssh fails (install step).
	if err := os.WriteFile(filepath.Join(binDir, "scp"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte("#!/bin/sh\necho fail >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	agentBin := filepath.Join(t.TempDir(), "grain-agent")
	if err := os.WriteFile(agentBin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := EnsureAgent(ctx, "127.0.0.1", 2222, "ubuntu", "", agentBin)
	if err == nil || !strings.Contains(err.Error(), "install") {
		t.Fatalf("want install error, got %v", err)
	}
}

func TestWaitSSHImmediateCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := WaitSSH(ctx, "127.0.0.1", 1, "u", "")
	if err == nil {
		t.Fatal("expected cancel error")
	}
}

func TestWaitSSHShortTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := WaitSSH(ctx, "127.0.0.1", 1, "root", "/no/key")
	if err == nil {
		t.Fatal("expected timeout")
	}
}

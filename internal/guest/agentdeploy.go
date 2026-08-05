package guest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GuestAgentBin is the install path of grain-agent inside the guest.
const GuestAgentBin = "/usr/local/bin/grain-agent"

// GuestAgentServicePath is the systemd unit path inside the guest.
const GuestAgentServicePath = "/etc/systemd/system/grain-agent.service"

// AgentServiceUnit is the systemd unit content installed into the guest.
// Kept in sync with deploy/agent/grain-agent.service.
const AgentServiceUnit = `[Unit]
Description=grain guest agent
After=network.target

[Service]
ExecStart=/usr/local/bin/grain-agent -listen :7475
Restart=on-failure

[Install]
WantedBy=multi-user.target
`

// EnsureAgent SCPs the Linux agent binary into the guest, installs a systemd
// unit, and enables/starts grain-agent. The caller should probe agent health
// first and skip this when the agent is already up (e.g. golden images).
//
// Requires working SSH (user typically has passwordless sudo via cloud-init).
// agentLinuxBinary must be a local path to a Linux-built grain-agent
// (see agent.LinuxBinaryPath / just agent-linux).
func EnsureAgent(ctx context.Context, host string, sshPort int, user, privKey, agentLinuxBinary string) error {
	if host == "" {
		return fmt.Errorf("ensure agent: empty host")
	}
	if user == "" {
		return fmt.Errorf("ensure agent: empty user")
	}
	if agentLinuxBinary == "" {
		return fmt.Errorf("ensure agent: empty binary path")
	}
	st, err := os.Stat(agentLinuxBinary)
	if err != nil {
		return fmt.Errorf("ensure agent: binary: %w", err)
	}
	if st.IsDir() {
		return fmt.Errorf("ensure agent: binary path is a directory: %s", agentLinuxBinary)
	}

	// Materialize unit file for scp.
	tmpDir, err := os.MkdirTemp("", "grain-agent-deploy-*")
	if err != nil {
		return fmt.Errorf("ensure agent: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	unitLocal := filepath.Join(tmpDir, "grain-agent.service")
	if err := os.WriteFile(unitLocal, []byte(AgentServiceUnit), 0o644); err != nil {
		return fmt.Errorf("ensure agent: write unit: %w", err)
	}

	remoteBin := "/tmp/grain-agent.new"
	remoteUnit := "/tmp/grain-agent.service"

	if err := scpToGuest(ctx, host, sshPort, user, privKey, agentLinuxBinary, remoteBin); err != nil {
		return fmt.Errorf("ensure agent: scp binary: %w", err)
	}
	if err := scpToGuest(ctx, host, sshPort, user, privKey, unitLocal, remoteUnit); err != nil {
		return fmt.Errorf("ensure agent: scp unit: %w", err)
	}

	// Install + enable. Prefer sudo when not root (grain user has NOPASSWD).
	script := strings.TrimSpace(`
set -e
if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi
$SUDO install -m 0755 /tmp/grain-agent.new `+GuestAgentBin+`
$SUDO cp /tmp/grain-agent.service `+GuestAgentServicePath+`
$SUDO systemctl daemon-reload
$SUDO systemctl enable grain-agent
# restart (not only enable --now) so a replaced binary is actually loaded
$SUDO systemctl restart grain-agent
rm -f /tmp/grain-agent.new /tmp/grain-agent.service
`) + "\n"

	if err := sshRun(ctx, host, sshPort, user, privKey, "sh", "-c", script); err != nil {
		return fmt.Errorf("ensure agent: install: %w", err)
	}
	return nil
}

func scpToGuest(ctx context.Context, host string, port int, user, identity, localPath, remotePath string) error {
	args := SCPArgs(port, identity)
	args = append(args, localPath, fmt.Sprintf("%s@%s:%s", user, host, remotePath))
	cmd := exec.CommandContext(ctx, "scp", args...)
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func sshRun(ctx context.Context, host string, port int, user, identity string, remote ...string) error {
	args := SSHArgs(user, host, port, identity)
	args = append(args, "--")
	args = append(args, remote...)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

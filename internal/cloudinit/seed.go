package cloudinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// WriteNoCloud builds a cloud-init NoCloud seed ISO (volume label cidata).
func WriteNoCloud(dir, hostname, sshPubLine, userdataExtra string) (seedPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	meta := fmt.Sprintf("instance-id: grain-%s\nlocal-hostname: %s\n", hostname, hostname)
	// Keep user-data minimal and valid YAML. Inject key for default distro user + grain.
	user := fmt.Sprintf(`#cloud-config
hostname: %s
fqdn: %s.local
manage_etc_hosts: true
ssh_pwauth: false
disable_root: false
users:
  - default
  - name: grain
    groups: [sudo, adm]
    shell: /bin/bash
    lock_passwd: true
    sudo: ["ALL=(ALL) NOPASSWD:ALL"]
    ssh_authorized_keys:
      - %s
# Also authorize the default cloud user (ubuntu/debian/etc.)
ssh_authorized_keys:
  - %s
runcmd:
  - [ sh, -c, "mkdir -p /home/ubuntu/.ssh /root/.ssh; echo '%s' >> /home/ubuntu/.ssh/authorized_keys; echo '%s' >> /root/.ssh/authorized_keys; chown -R ubuntu:ubuntu /home/ubuntu/.ssh 2>/dev/null || true; chmod 600 /home/ubuntu/.ssh/authorized_keys /root/.ssh/authorized_keys 2>/dev/null || true" ]
  - [ sh, -c, "echo grain-ready > /var/lib/grain-ready" ]
%s
`, hostname, hostname,
		strings.TrimSpace(sshPubLine),
		strings.TrimSpace(sshPubLine),
		strings.TrimSpace(sshPubLine),
		strings.TrimSpace(sshPubLine),
		userdataExtra,
	)

	seedDir := filepath.Join(dir, "cidata")
	_ = os.RemoveAll(seedDir)
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(seedDir, "meta-data"), []byte(meta), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(seedDir, "user-data"), []byte(user), 0o644); err != nil {
		return "", err
	}
	_ = os.WriteFile(filepath.Join(seedDir, "vendor-data"), []byte(""), 0o644)

	iso := filepath.Join(dir, "seed.iso")
	_ = os.Remove(iso)
	if err := makeISO(seedDir, iso); err != nil {
		return "", err
	}
	return iso, nil
}

func makeISO(srcDir, destISO string) error {
	if runtime.GOOS == "darwin" {
		// ISO9660 + Joliet only (no HFS) so cloud-init reliably sees volid cidata
		cmd := exec.Command("hdiutil", "makehybrid",
			"-o", destISO,
			"-iso",
			"-joliet",
			"-default-volume-name", "cidata",
			srcDir,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("hdiutil: %w (%s)", err, string(out))
		}
		return nil
	}
	for _, bin := range []string{"genisoimage", "mkisofs", "xorriso"} {
		if _, err := exec.LookPath(bin); err != nil {
			continue
		}
		var cmd *exec.Cmd
		if bin == "xorriso" {
			cmd = exec.Command(bin, "-as", "mkisofs", "-output", destISO, "-volid", "cidata", "-joliet", "-rock", srcDir)
		} else {
			cmd = exec.Command(bin, "-output", destISO, "-volid", "cidata", "-joliet", "-rock", "-input-charset", "utf-8", srcDir)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w (%s)", bin, err, string(out))
		}
		return nil
	}
	return fmt.Errorf("no ISO tool found (need hdiutil, genisoimage, mkisofs, or xorriso)")
}

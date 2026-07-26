package cloudinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// WriteNoCloud builds a cloud-init seed (cidata) for first boot.
// Returns path to seed.iso or seed.img usable as a second drive.
func WriteNoCloud(dir, hostname, sshPubLine, userdataExtra string) (seedPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	meta := fmt.Sprintf("instance-id: grain-%s\nlocal-hostname: %s\n", hostname, hostname)
	user := fmt.Sprintf(`#cloud-config
hostname: %s
manage_etc_hosts: true
users:
  - default
  - name: grain
    gecos: grain
    groups: [sudo, adm]
    shell: /bin/bash
    sudo: ALL=(ALL) NOPASSWD:ALL
    lock_passwd: true
    ssh_authorized_keys:
      - %s
ssh_pwauth: false
package_update: false
runcmd:
  - [ sh, -c, "echo grain-ready > /var/lib/grain-ready" ]
%s
`, hostname, strings.TrimSpace(sshPubLine), userdataExtra)

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
	// empty vendor-data helps some images
	_ = os.WriteFile(filepath.Join(seedDir, "vendor-data"), []byte("#cloud-config\n"), 0o644)

	iso := filepath.Join(dir, "seed.iso")
	if err := makeISO(seedDir, iso); err != nil {
		return "", err
	}
	return iso, nil
}

func makeISO(srcDir, destISO string) error {
	// Prefer genisoimage / mkisofs / xorriso; on macOS use hdiutil.
	if runtime.GOOS == "darwin" {
		// hdiutil makehybrid
		cmd := exec.Command("hdiutil", "makehybrid", "-o", destISO, "-hfs", "-joliet", "-iso", "-default-volume-name", "cidata", srcDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			// already exists?
			_ = os.Remove(destISO)
			cmd = exec.Command("hdiutil", "makehybrid", "-o", destISO, "-hfs", "-joliet", "-iso", "-default-volume-name", "cidata", srcDir)
			out, err = cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("hdiutil: %w (%s)", err, string(out))
			}
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
			cmd = exec.Command(bin, "-output", destISO, "-volid", "cidata", "-joliet", "-rock", srcDir)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w (%s)", bin, err, string(out))
		}
		return nil
	}
	return fmt.Errorf("no ISO tool found (need hdiutil, genisoimage, mkisofs, or xorriso)")
}

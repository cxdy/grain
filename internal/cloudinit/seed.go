package cloudinit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// WriteNoCloud builds a cloud-init NoCloud seed ISO (volume label cidata).
// userdataExtra is optional shell or #cloud-config merged into the base document.
// mounts, when non-empty, inject runcmd entries to mount each virtio-9p share.
func WriteNoCloud(dir, hostname, sshPubLine, userdataExtra string, mounts ...MountSpec) (seedPath string, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	meta := fmt.Sprintf("instance-id: grain-%s\nlocal-hostname: %s\n", hostname, hostname)

	user, err := RenderUserData(hostname, sshPubLine, userdataExtra, mounts...)
	if err != nil {
		return "", fmt.Errorf("cloud-init user-data: %w", err)
	}

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

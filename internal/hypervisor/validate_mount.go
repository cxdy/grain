package hypervisor

import (
	"fmt"
	"regexp"
	"unicode"

	"github.com/cxdy/grain/internal/vm"
)

// mountTagRe is the allowed QEMU mount_tag / virtiofs tag alphabet.
// Matches grainN defaults and safe custom tags; rejects commas, spaces, and
// other characters that would break or inject into QEMU option strings.
var mountTagRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)

// ValidateMount checks that a host directory share is safe to embed in QEMU
// -fsdev / -device option strings (and virtiofs tags).
//
// Host path must not contain ',', '=', NUL, CR, LF, or other control characters
// (QEMU option lists are comma-separated key=value pairs).
// Tag must match ^[a-zA-Z0-9._-]{1,64}$.
func ValidateMount(m vm.Mount) error {
	if err := validateMountHost(m.Host); err != nil {
		return err
	}
	if err := validateMountTag(m.Tag); err != nil {
		return err
	}
	return nil
}

// ValidateMounts validates every mount in the list.
func ValidateMounts(mounts []vm.Mount) error {
	for i, m := range mounts {
		if err := ValidateMount(m); err != nil {
			return fmt.Errorf("mount[%d]: %w", i, err)
		}
	}
	return nil
}

func validateMountHost(host string) error {
	if host == "" {
		return fmt.Errorf("mount: empty host path")
	}
	for _, r := range host {
		if r == ',' || r == '=' || r == 0 || unicode.IsControl(r) {
			return fmt.Errorf("mount: host path contains forbidden character")
		}
	}
	return nil
}

func validateMountTag(tag string) error {
	if !mountTagRe.MatchString(tag) {
		return fmt.Errorf("mount: invalid tag")
	}
	return nil
}

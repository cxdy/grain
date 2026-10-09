//go:build darwin

package hypervisor

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// processExecutable returns the executable path for pid via kern.procargs2.
// Darwin LP64 layout: int32 argc, optional NUL padding, then a NUL-terminated path.
func processExecutable(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("bad pid %d", pid)
	}
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	if len(buf) < 4 {
		return "", fmt.Errorf("kern.procargs2 short for pid %d", pid)
	}
	rest := buf[4:]
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	for i, b := range rest {
		if b == 0 {
			if i == 0 {
				return "", fmt.Errorf("empty executable for pid %d", pid)
			}
			return string(rest[:i]), nil
		}
	}
	if len(rest) == 0 {
		return "", fmt.Errorf("empty executable for pid %d", pid)
	}
	return string(rest), nil
}

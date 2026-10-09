//go:build linux

package hypervisor

import (
	"fmt"
	"os"
	"strings"
)

func processExecutable(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("bad pid %d", pid)
	}
	p, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", err
	}
	p = strings.TrimSuffix(p, " (deleted)")
	if p == "" {
		return "", fmt.Errorf("empty executable for pid %d", pid)
	}
	return p, nil
}

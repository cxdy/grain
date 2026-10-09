//go:build !linux && !darwin

package hypervisor

import "fmt"

func processExecutable(pid int) (string, error) {
	return "", fmt.Errorf("process executable unavailable for pid %d", pid)
}

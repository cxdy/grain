//go:build unix

package netutil

import (
	"errors"
	"syscall"
)

func errnoHostUnreachable(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EHOSTUNREACH || errno == syscall.ENETUNREACH
	}
	return false
}

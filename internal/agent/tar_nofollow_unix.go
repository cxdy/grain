//go:build unix

package agent

import "syscall"

// tarNoFollow is OR'd into put-tar regular-file open flags so the final path
// component is never followed when it is a symlink.
const tarNoFollow = syscall.O_NOFOLLOW

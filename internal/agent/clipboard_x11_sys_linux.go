//go:build linux

package agent

import "syscall"

func x11SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

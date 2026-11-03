//go:build !windows && !linux

package platform

import "syscall"

func sysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

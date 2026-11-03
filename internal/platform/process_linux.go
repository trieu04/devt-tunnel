package platform

import "syscall"

// Pdeathsig ends the child if the app dies, on top of the check made on the next launch.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

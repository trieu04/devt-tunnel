package platform

import "syscall"

// Process records handle crash recovery; Pdeathsig is avoided because it follows the forking OS thread.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

func shutdownSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP} }
func isHangup(sig os.Signal) bool  { return sig == syscall.SIGHUP }

package daemon

import "os"

func shutdownSignals() []os.Signal { return []os.Signal{os.Interrupt} }
func isHangup(os.Signal) bool      { return false }

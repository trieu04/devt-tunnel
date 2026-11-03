//go:build !linux

package host

func trayAvailable() bool { return true }

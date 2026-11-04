//go:build !linux && !darwin && !windows

package platform

import "net"

func CheckPeer(net.Conn) error { return nil }
